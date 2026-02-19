package spot

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	storageSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"

	"github.com/jackc/pgx/v5/pgxpool"
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
		log.Printf("SpotInstrumentService listening on %s", app.config.Address)

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

	logger := logInterceptor.LoggerInterceptor()

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(validator, logger),
	)

	marketStore := storageSpot.NewMarketStore(app.dbPool)
	useCase := svcSpot.NewService(marketStore)
	grpcSpot.Register(app.grpcServer, useCase)
	reflection.Register(app.grpcServer)

	return nil
}

func (app *App) Stop() error {
	if app.grpcServer != nil {
		app.grpcServer.GracefulStop()
	}

	if app.dbPool != nil {
		app.dbPool.Close()
	}

	return nil
}
