package order

import (
	"context"
	"fmt"
	"log"
	"net"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	storageOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/postgres"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer     *grpc.Server
	listener       net.Listener
	spotConnection *grpc.ClientConn
	dbPool         *pgxpool.Pool
	config         config.OrderConfig
}

func New(cfg config.OrderConfig) *App {
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

	if err := app.setupSpotConnection(); err != nil {
		return nil, fmt.Errorf("setupSpotConnection: %w", err)
	}

	marketClient := grpcClient.New(app.spotConnection, app.config.CircuitBreaker)

	if err := app.checkSpotReachable(ctx, marketClient); err != nil {
		return nil, fmt.Errorf("checkSpotReachable: %w", err)
	}

	if err := app.setupGRPCServer(marketClient); err != nil {
		return nil, fmt.Errorf("setupGRPCServer: %w", err)
	}

	errChan := make(chan error, 1)

	go func() {
		log.Printf("OrderService listening on %s (spot: %s)", app.config.Address, app.config.SpotAddress)

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

func (app *App) setupSpotConnection() error {
	connection, err := grpc.NewClient(
		app.config.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return fmt.Errorf("grpc.NewClient: %w", err)
	}

	app.spotConnection = connection
	return nil
}

func (app *App) checkSpotReachable(ctx context.Context, client *grpcClient.Client) error {
	ctx, cancel := context.WithTimeout(ctx, app.config.CheckTimeout)
	defer cancel()

	if _, err := client.ViewMarkets(ctx, []models.UserRole{models.UserRoleUser}); err != nil {
		return fmt.Errorf("spot instrument is not reachable at %s: %w", app.config.SpotAddress, err)
	}

	return nil
}

func (app *App) setupGRPCServer(marketClient *grpcClient.Client) error {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	logger := logInterceptor.LoggerInterceptor()

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(validator, logger),
	)

	orderStore := storageOrder.NewOrderStore(app.dbPool)
	useCase := svcOrder.NewService(orderStore, orderStore, marketClient, app.config.CreateTimeout)
	grpcOrder.Register(app.grpcServer, useCase)
	reflection.Register(app.grpcServer)

	return nil
}

func (app *App) Stop() error {
	if app.grpcServer != nil {
		app.grpcServer.GracefulStop()
	}

	if app.spotConnection != nil {
		if err := app.spotConnection.Close(); err != nil {
			log.Printf("failed to close spot connection: %v", err)
		}
	}

	if app.dbPool != nil {
		app.dbPool.Close()
	}

	return nil
}
