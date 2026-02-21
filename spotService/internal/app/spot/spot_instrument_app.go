package spot

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/closer"
	postgres "github.com/nastyazhadan/spot-order-grpc/shared/infra/db"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/health"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
)

type App struct {
	diContainer *DiContainer

	grpcServer *grpc.Server
	listener   net.Listener

	dbPool *pgxpool.Pool
	config config.SpotConfig
}

func New(ctx context.Context, cfg config.SpotConfig) (*App, error) {
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

func (app *App) setupDI(_ context.Context) error {
	app.diContainer = NewDIContainer(app.dbPool)

	return nil
}

func (app *App) setupLogger(_ context.Context) error {
	return zapLogger.Init(
		app.config.LogLevel,
		app.config.LogFormat == "json",
	)
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

func (app *App) setupCloser(_ context.Context) error {
	closer.SetLogger(zapLogger.Logger())

	closer.AddNamed("zap logger sync", func(ctx context.Context) error {
		zapLogger.Sync()
		return nil
	})

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

	grpcSpot.Register(app.grpcServer, app.diContainer.SpotService(ctx))

	return nil
}

func (app *App) runGRPCServer(ctx context.Context) error {
	zapLogger.Info(ctx, fmt.Sprintf("Starting gRPC Spot Server on %s", app.config.Address))

	err := app.grpcServer.Serve(app.listener)
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("grpcServer.Serve: %w", err)
	}

	return nil
}
