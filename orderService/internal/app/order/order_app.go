package order

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	storageOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/postgres"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres/migrator"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener

	address     string
	spotAddress string

	spotConnection *grpc.ClientConn
	pool           *pgxpool.Pool
}

func New(address string) (*App, error) {
	if address == "" {
		address = "127.0.0.1:50051"
	}

	spotAddress := os.Getenv("SPOT_INSTRUMENT_ADDR")
	if spotAddress == "" {
		spotAddress = "127.0.0.1:50052"
	}

	return &App{
		address:     address,
		spotAddress: spotAddress,
	}, nil
}

func (app *App) Start() error {
	ctx := context.Background()

	pool, err := app.setupDB(ctx)
	if err != nil {
		return fmt.Errorf("setupDB: %w", err)
	}
	app.pool = pool

	listener, err := net.Listen("tcp", app.address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", app.address, err)
	}
	app.listener = listener

	spotConnection, err := grpc.NewClient(
		app.spotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial spot instrument %s: %w", app.spotAddress, err)
	}
	app.spotConnection = spotConnection

	marketClient := grpcClient.New(app.spotConnection)

	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("proto validate: %w", err)
	}

	logger := logInterceptor.LoggerInterceptor()

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			validator,
			logger,
		),
	)

	orderStore := storageOrder.NewOrderStore(pool)
	useCase := svcOrder.NewService(orderStore, orderStore, marketClient)
	grpcOrder.Register(app.grpcServer, useCase)

	reflection.Register(app.grpcServer) // для отладки

	{
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if _, err := marketClient.ViewMarkets(ctx, []models.UserRole{}); err != nil {
			return fmt.Errorf("spot instrument is not reachable at %s: %w", app.spotAddress, err)
		}
	}

	go func() {
		log.Printf("OrderService listening on %s (spot instrument: %s)", app.address, app.spotAddress)

		if err := app.grpcServer.Serve(app.listener); err != nil {
			log.Printf("failed to serve: %v\n", err)
		}
	}()

	return nil
}

func (app *App) setupDB(ctx context.Context) (*pgxpool.Pool, error) {
	dbURI := os.Getenv("ORDER_DB_URI")
	if dbURI == "" {
		return nil, fmt.Errorf("ORDER_DB_URI environment variable is not set")
	}

	pool, err := postgres.NewPgxPool(ctx, dbURI)
	if err != nil {
		return nil, fmt.Errorf("storage.NewPgxPool: %w", err)
	}

	sqlDB, err := sql.Open("pgx", dbURI)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	defer sqlDB.Close()

	dbMigrator := migrator.NewMigrator(sqlDB, os.Getenv("MIGRATION_DIR"))
	if err := dbMigrator.Up(); err != nil {
		return nil, fmt.Errorf("migrator.Up: %w", err)
	}

	return pool, nil
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

	if app.pool != nil {
		app.pool.Close()
	}

	return nil
}
