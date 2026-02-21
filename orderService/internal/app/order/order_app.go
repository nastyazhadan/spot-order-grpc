package order

import (
	"context"
	"errors"
	"fmt"
	"net"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	postgres "github.com/nastyazhadan/spot-order-grpc/shared/infra/db"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/health"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/closer"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type App struct {
	diContainer *DiContainer

	grpcServer *grpc.Server
	listener   net.Listener

	spotConnection *grpc.ClientConn
	marketClient   *grpcClient.Client

	dbPool *pgxpool.Pool
	config config.OrderConfig
}

func New(ctx context.Context, cfg config.OrderConfig) (*App, error) {
	app := &App{
		config: cfg,
	}

	err := app.setupDeps(ctx)
	if err != nil {
		return nil, err
	}

	return app, nil
}

func (app *App) Start(ctx context.Context) error {
	return app.runGRPCServer(ctx)
}

func (app *App) setupDeps(ctx context.Context) error {
	setups := []func(ctx context.Context) error{
		app.setupLogger,
		app.setupCloser,
		app.setupDB,
		app.setupSpotConnection,
		app.setupMarketClient,
		app.setupDI,
		app.setupListener,
		app.setupGRPCServer,
	}

	for _, init := range setups {
		if err := init(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (app *App) setupLogger(_ context.Context) error {
	return zapLogger.Init(
		app.config.LogLevel,
		app.config.LogFormat == "json",
	)
}

func (app *App) setupCloser(_ context.Context) error {
	closer.SetLogger(zapLogger.Logger())

	closer.AddNamed("zap logger sync", func(ctx context.Context) error {
		zapLogger.Sync()
		return nil
	})

	return nil
}

func (app *App) setupDB(ctx context.Context) error {
	pool, err := postgres.SetupDB(ctx, app.config.DBURI, migrations.Migrations)
	if err != nil {
		return fmt.Errorf("postgres.SetupDB: %w", err)
	}

	app.dbPool = pool

	closer.AddNamed("Postgres pool", func(ctx context.Context) error {
		app.dbPool.Close()
		return nil
	})

	return nil
}

func (app *App) setupSpotConnection(_ context.Context) error {
	connection, err := grpc.NewClient(
		app.config.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(xrequestid.Client),
	)

	if err != nil {
		return fmt.Errorf("grpc.NewClient: %w", err)
	}

	app.spotConnection = connection

	closer.AddNamed("Spot gRPC connection", func(ctx context.Context) error {
		return app.spotConnection.Close()
	})

	return nil
}

func (app *App) setupMarketClient(ctx context.Context) error {
	app.marketClient = grpcClient.New(app.spotConnection, app.config.CircuitBreaker)

	if err := app.checkSpotConnection(ctx, app.marketClient); err != nil {
		return fmt.Errorf("checkSpotConnection: %w", err)
	}

	return nil
}

func (app *App) setupDI(_ context.Context) error {
	app.diContainer = NewDIContainer(app.dbPool, app.marketClient, app.config.CreateTimeout)

	return nil
}

func (app *App) setupListener(_ context.Context) error {
	listener, err := net.Listen("tcp", app.config.Address)
	if err != nil {
		return fmt.Errorf("net.Listen: %w", err)
	}

	app.listener = listener

	closer.AddNamed("TCP listener", func(ctx context.Context) error {
		l := listener.Close()
		if l != nil && !errors.Is(l, net.ErrClosed) {
			return l
		}

		return nil
	})

	return nil
}

func (app *App) checkSpotConnection(ctx context.Context, client *grpcClient.Client) error {
	ctx, cancel := context.WithTimeout(ctx, app.config.CheckTimeout)
	defer cancel()

	if _, err := client.ViewMarkets(ctx, []models.UserRole{models.UserRoleUser}); err != nil {
		return fmt.Errorf("spot instrument is not reachable at %s: %w", app.config.SpotAddress, err)
	}

	return nil
}

func (app *App) setupGRPCServer(ctx context.Context) error {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	tracer := xrequestid.Server
	logger := logInterceptor.LoggerInterceptor()
	recoverer := recovery.PanicRecoveryInterceptor

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tracer,
			logger,
			recoverer,
			validator,
		),
	)

	closer.AddNamed("gRPC Server", func(ctx context.Context) error {
		app.grpcServer.GracefulStop()
		return nil
	})

	reflection.Register(app.grpcServer)

	health.RegisterService(app.grpcServer)

	grpcOrder.Register(app.grpcServer, app.diContainer.OrderService(ctx))

	return nil
}

func (app *App) runGRPCServer(ctx context.Context) error {
	zapLogger.Info(ctx, fmt.Sprintf("Starting gRPC Order Server on %s", app.config.Address))

	err := app.grpcServer.Serve(app.listener)
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("grpcServer.Serve: %w", err)
	}

	return nil
}
