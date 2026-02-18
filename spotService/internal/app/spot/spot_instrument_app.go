package spot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres/migrator"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	storageSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener
	address    string
	pool       *pgxpool.Pool
}

func New(address string) (*App, error) {
	if address == "" {
		address = ":50052"
	}

	return &App{
		address: address,
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

	marketStore := storageSpot.NewMarketStore(pool)
	useCase := svcSpot.NewService(marketStore)
	grpcSpot.Register(app.grpcServer, useCase)

	reflection.Register(app.grpcServer) // для отладки

	go func() {
		log.Printf("SpotInstrumentService listening on %s", app.address)

		if err := app.grpcServer.Serve(app.listener); err != nil {
			log.Printf("failed to serve: %v\n", err)
		}
	}()

	return nil
}

func (app *App) setupDB(ctx context.Context) (*pgxpool.Pool, error) {
	dbURI := os.Getenv("SPOT_DB_URI")
	if dbURI == "" {
		return nil, fmt.Errorf("SPOT_DB_URI environment variable is not set")
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

	if app.pool != nil {
		app.pool.Close()
	}

	return nil
}
