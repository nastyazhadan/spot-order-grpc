//go:build integration

package suite

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	pgContainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	migrate "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db/migrator"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
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

	redisConnectionTimeout = 3 * time.Second
	redisCacheKey          = "market:cache:all"
)

type Suite struct {
	Test       *testing.T
	SpotClient proto.SpotInstrumentServiceClient
	Pool       *pgxpool.Pool
	RedisPool  *redigo.Pool
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

	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForListeningPort("6379/tcp").
				WithStartupTimeout(startupTimeout),
		},
	})
	if err != nil {
		test.Fatalf("failed to start redis container: %v", err)
	}
	test.Cleanup(func() {
		if err := redisC.Terminate(context.Background()); err != nil {
			test.Logf("failed to terminate redis container: %v", err)
		}
	})

	redisHost, err := redisC.Host(ctx)
	if err != nil {
		test.Fatalf("failed to get redis host: %v", err)
	}
	redisPort, err := redisC.MappedPort(ctx, "6379/tcp")
	if err != nil {
		test.Fatalf("failed to get redis port: %v", err)
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	redisPool := &redigo.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redigo.Conn, error) {
			return redigo.Dial("tcp", redisAddr)
		},
		TestOnBorrow: func(c redigo.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
	test.Cleanup(func() {
		_ = redisPool.Close()
	})

	redisClient := cache.NewClient(redisPool, zapLogger.With(), redisConnectionTimeout)

	marketRepo := repoPostgres.NewMarketStore(pool)
	cacheRepo := repoRedis.NewMarketCacheRepository(redisClient)
	marketSvc := svcSpot.NewService(marketRepo, cacheRepo, cacheTTL)

	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		test.Fatalf("validate.ProtovalidateUnary: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recovery.PanicRecoveryInterceptor,
			validator,
		),
	)
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
		test.Fatalf("failed to connect to gRPC server: %v", err)
	}
	test.Cleanup(func() {
		_ = connection.Close()
	})

	return ctx, &Suite{
		Test:       test,
		SpotClient: proto.NewSpotInstrumentServiceClient(connection),
		Pool:       pool,
		RedisPool:  redisPool,
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
