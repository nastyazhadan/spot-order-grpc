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
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/closer"
	postgres "github.com/nastyazhadan/spot-order-grpc/shared/infra/db"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/health"
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

func (a *App) Start(ctx context.Context) error {
	return a.runGRPCServer(ctx)
}

func (a *App) setupDeps(ctx context.Context) error {
	setups := []func(ctx context.Context) error{
		a.setupLogger,
		a.setupCloser,
		a.setupDB,
		a.setupSpotConnection,
		a.setupMarketClient,
		a.setupDI,
		a.setupListener,
		a.setupGRPCServer,
	}

	for _, init := range setups {
		if err := init(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) setupLogger(_ context.Context) error {
	return zapLogger.Init(
		a.config.LogLevel,
		a.config.LogFormat == "json",
	)
}

func (a *App) setupCloser(_ context.Context) error {
	closer.SetLogger(zapLogger.Logger())

	closer.AddNamed("zap logger sync", func(ctx context.Context) error {
		zapLogger.Sync()
		return nil
	})

	return nil
}

func (a *App) setupDB(ctx context.Context) error {
	pool, err := postgres.SetupDB(ctx, a.config.DBURI, migrations.Migrations)
	if err != nil {
		return fmt.Errorf("postgres.SetupDB: %w", err)
	}

	a.dbPool = pool

	closer.AddNamed("Postgres pool", func(ctx context.Context) error {
		a.dbPool.Close()
		return nil
	})

	return nil
}

func (a *App) setupSpotConnection(_ context.Context) error {
	connection, err := grpc.NewClient(
		a.config.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(xrequestid.Client),
	)

	if err != nil {
		return fmt.Errorf("grpc.NewClient: %w", err)
	}

	a.spotConnection = connection

	closer.AddNamed("Spot gRPC connection", func(ctx context.Context) error {
		return a.spotConnection.Close()
	})

	return nil
}

func (a *App) setupMarketClient(ctx context.Context) error {
	a.marketClient = grpcClient.New(a.spotConnection, a.config.CircuitBreaker)

	if err := a.checkSpotConnection(ctx, a.marketClient); err != nil {
		return fmt.Errorf("checkSpotConnection: %w", err)
	}

	return nil
}

func (a *App) setupDI(_ context.Context) error {
	a.diContainer = NewDIContainer(
		a.dbPool,
		a.marketClient,
		a.config,
	)

	closer.AddNamed("Redis pool", func(ctx context.Context) error {
		return a.diContainer.RedisPool().Close()
	})

	return nil
}

func (a *App) setupListener(_ context.Context) error {
	listener, err := net.Listen("tcp", a.config.Address)
	if err != nil {
		return fmt.Errorf("net.Listen: %w", err)
	}

	a.listener = listener

	closer.AddNamed("TCP listener", func(ctx context.Context) error {
		l := listener.Close()
		if l != nil && !errors.Is(l, net.ErrClosed) {
			return l
		}

		return nil
	})

	return nil
}

func (a *App) checkSpotConnection(ctx context.Context, client *grpcClient.Client) error {
	ctx, cancel := context.WithTimeout(ctx, a.config.CheckTimeout)
	defer cancel()

	if _, err := client.ViewMarkets(ctx, []models.UserRole{models.UserRoleUser}); err != nil {
		return fmt.Errorf("spot instrument is not reachable at %s: %w", a.config.SpotAddress, err)
	}

	return nil
}

func (a *App) setupGRPCServer(ctx context.Context) error {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	tracer := xrequestid.Server
	logger := logInterceptor.LoggerInterceptor()
	recoverer := recovery.PanicRecoveryInterceptor

	a.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tracer,
			logger,
			recoverer,
			validator,
		),
	)

	closer.AddNamed("gRPC Server", func(ctx context.Context) error {
		a.grpcServer.GracefulStop()
		return nil
	})

	reflection.Register(a.grpcServer)
	health.RegisterService(a.grpcServer)
	grpcOrder.Register(a.grpcServer, a.diContainer.OrderService(ctx))

	return nil
}

func (a *App) runGRPCServer(ctx context.Context) error {
	zapLogger.Info(ctx, fmt.Sprintf("Starting gRPC Order Server on %s", a.config.Address))

	err := a.grpcServer.Serve(a.listener)
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("grpcServer.Serve: %w", err)
	}

	return nil
}
