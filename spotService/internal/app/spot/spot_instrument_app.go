package spot

import (
	"context"
	"fmt"
	"net"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	storageSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"

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
	return &App{config: cfg}
}

func (app *App) Start() (<-chan error, error) {
	ctx := context.Background()

	if err := zapLogger.Init("info", false); err != nil {
		return nil, fmt.Errorf("zapLogger.Init: %w", err)
	}

	if err := app.setupDB(ctx); err != nil {
		return nil, fmt.Errorf("setupDB: %w", err)
	}

	if err := app.setupListener(); err != nil {
		return nil, fmt.Errorf("setupListener: %w", err)
	}

	if err := app.setupGRPCServer(); err != nil {
		return nil, fmt.Errorf("setupGRPCServer: %w", err)
	}

	errChan := make(chan error, 1)

	go func() {
		zapLogger.Info(context.Background(), "Spot service started",
			zap.String("address", app.config.Address),
		)

		if err := app.grpcServer.Serve(app.listener); err != nil {
			errChan <- err
		}
	}()

	return errChan, nil
}

func (app *App) setupDB(ctx context.Context) error {
	pool, err := postgres.SetupDB(ctx, app.config.DBURI, app.config.MigrationDir)
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

func (app *App) setupGRPCServer() error {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			xrequestid.Server,
			logInterceptor.LoggerInterceptor(),
			recovery.Unary,
			validator,
		),
	)

	marketStore := storageSpot.NewMarketStore(app.dbPool)
	useCase := svcSpot.NewService(marketStore)
	grpcSpot.Register(app.grpcServer, useCase)
	reflection.Register(app.grpcServer)

	return nil
}

func (app *App) Stop() error {
	defer zapLogger.Sync()

	if app.grpcServer != nil {
		app.grpcServer.GracefulStop()
	}

	if app.dbPool != nil {
		app.dbPool.Close()
	}

	return nil
}
