package spot

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/closer"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
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

func (a *App) Start(ctx context.Context) error {
	return a.runGRPCServer(ctx)
}

func (a *App) setupDeps(ctx context.Context) error {
	setups := []func(ctx context.Context) error{
		a.setupLogger,
		a.setupCloser,
		a.setupDB,
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

func (a *App) setupDI(_ context.Context) error {
	a.diContainer = NewDIContainer(a.dbPool, a.config)

	return nil
}

func (a *App) setupLogger(_ context.Context) error {
	return zapLogger.Init(
		a.config.LogLevel,
		a.config.LogFormat == "json",
	)
}

func (a *App) setupDB(ctx context.Context) error {
	pool, err := db.SetupDB(ctx, a.config.DBURI, migrations.Migrations)
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

func (a *App) setupCloser(_ context.Context) error {
	closer.SetLogger(zapLogger.Logger())

	closer.AddNamed("zap logger sync", func(ctx context.Context) error {
		zapLogger.Sync()
		return nil
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
	grpcSpot.Register(a.grpcServer, a.diContainer.SpotService(ctx))

	return nil
}

func (a *App) runGRPCServer(ctx context.Context) error {
	zapLogger.Info(ctx, fmt.Sprintf("Starting gRPC Spot Server on %s", a.config.Address))

	err := a.grpcServer.Serve(a.listener)
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("grpcServer.Serve: %w", err)
	}

	return nil
}
