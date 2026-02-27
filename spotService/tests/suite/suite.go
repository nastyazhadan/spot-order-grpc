//go:build integration

package suite

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	pgContainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	migrate "github.com/nastyazhadan/spot-order-grpc/shared/infra/db/migrator"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v1"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	repoSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
)

const (
	dbUser     = "test_user"
	dbPassword = "test_password"
	dbName     = "spot_test_db"

	longTimeout    = 2 * time.Minute
	startupTimeout = 30 * time.Second
	cacheTTL       = 5 * time.Minute
)

type Suite struct {
	Test       *testing.T
	SpotClient proto.SpotInstrumentServiceClient
	Pool       *pgxpool.Pool
}

type NoopMarketCache struct{}

func (n *NoopMarketCache) GetAll(ctx context.Context) ([]models.Market, error) {
	return nil, repositoryErrors.ErrMarketCacheNotFound
}

func (n *NoopMarketCache) SetAll(ctx context.Context, m []models.Market, ttl time.Duration) error {
	return nil
}

func New(test *testing.T) (context.Context, *Suite) {
	test.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), longTimeout)
	test.Cleanup(cancel)

	container, err := pgContainer.Run(ctx,
		"postgres:17.0-alpine3.20",
		pgContainer.WithDatabase(dbName),
		pgContainer.WithUsername(dbUser),
		pgContainer.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(startupTimeout),
		),
	)
	if err != nil {
		test.Fatalf("failed to start postgres container: %v", err)
	}
	test.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			test.Logf("failed to terminate postgres container: %v", err)
		}
	})

	network, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		test.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, network)
	if err != nil {
		test.Fatalf("failed to create pgxpool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		test.Fatalf("failed to ping postgres: %v", err)
	}
	test.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDBFromPool(pool)
	test.Cleanup(func() {
		_ = sqlDB.Close()
	})

	migrator := migrate.NewMigrator(sqlDB, migrations.Migrations)
	if err := migrator.Up(ctx); err != nil {
		test.Fatalf("failed to run migrations: %v", err)
	}

	marketRepo := repoSpot.NewMarketStore(pool)
	cacheRepo := &NoopMarketCache{} // пока заглушка
	marketSvc := svcSpot.NewService(marketRepo, cacheRepo, cacheTTL)

	grpcServer := grpc.NewServer()
	grpcSpot.Register(grpcServer, marketSvc)

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		test.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	test.Cleanup(func() {
		grpcServer.GracefulStop()
	})

	address := fmt.Sprintf("localhost:%d", listener.Addr().(*net.TCPAddr).Port)
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		test.Fatalf("failed to connect to grpc server: %v", err)
	}
	test.Cleanup(func() {
		_ = connection.Close()
	})

	return ctx, &Suite{
		Test:       test,
		SpotClient: proto.NewSpotInstrumentServiceClient(connection),
		Pool:       pool,
	}
}

func (s *Suite) ClearMarkets(ctx context.Context) {
	s.Test.Helper()
	if _, err := s.Pool.Exec(ctx, "DELETE FROM market_store"); err != nil {
		s.Test.Fatalf("failed to clear market_store: %v", err)
	}
}

func (s *Suite) InsertMarket(ctx context.Context, id, name string, enabled bool, deletedAt *time.Time) {
	s.Test.Helper()
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO market_store (id, name, enabled, deleted_at) VALUES ($1, $2, $3, $4)`,
		id, name, enabled, deletedAt,
	)

	if err != nil {
		s.Test.Fatalf("failed to insert market: %v", err)
	}
}
