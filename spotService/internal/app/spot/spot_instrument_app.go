package spot

import (
	"context"
	"fmt"
	"net"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	postgres "github.com/nastyazhadan/spot-order-grpc/shared/infra/db"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/health"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	storageSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener
	dbPool     *pgxpool.Pool
	config     config.SpotConfig
}

func New(cfg config.SpotConfig) *App {
	return &App{
		config: cfg,
	}
}

func (app *App) Start(ctx context.Context) error {
	zapLogger.Init(app.config.LogLevel, app.config.LogFormat == "json")
	defer zapLogger.Sync()

	if err := app.setupDB(ctx); err != nil {
		return fmt.Errorf("setupDB: %w", err)
	}

	if err := app.setupListener(); err != nil {
		return fmt.Errorf("setupListener: %w", err)
	}

	if err := app.setupGRPCServer(ctx); err != nil {
		return fmt.Errorf("setupGRPCServer: %w", err)
	}

	go func() {
		zapLogger.Info(ctx, "Spot service started",
			zap.String("address", app.config.Address),
		)

		if err := app.grpcServer.Serve(app.listener); err != nil {
			zapLogger.Fatal(ctx, "grpcServer.Serve:",
				zap.Error(err))
		}
	}()

	return nil
}

func (app *App) setupDB(ctx context.Context) error {
	pool, err := postgres.SetupDB(ctx, app.config.DBURI, migrations.Migrations)
	if err != nil {
		return err
	}

	app.dbPool = pool
	return nil
}

func (app *App) setupListener() error {
	listener, err := net.Listen("tcp", app.config.Address)
	if err != nil {
		return fmt.Errorf("net.Listen: %w", err)
	}

	app.listener = listener
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

	reflection.Register(app.grpcServer)

	health.RegisterService(app.grpcServer)

	marketStore := storageSpot.NewMarketStore(app.dbPool)
	useCase := svcSpot.NewService(marketStore)
	grpcSpot.Register(app.grpcServer, useCase)

	return nil
}
