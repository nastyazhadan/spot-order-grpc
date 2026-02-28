//go:build integration

package suite

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	pgContainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	migrate "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db/migrator"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

const (
	dbUser     = "test_user"
	dbPassword = "test_password"
	dbName     = "order_test_db"

	defaultCreateTimeout = 5 * time.Second
	longTimeout          = 2 * time.Minute
	startupTimeout       = 30 * time.Second

	createRateLimit  int64         = 1_000_000
	createRateWindow time.Duration = 1 * time.Hour

	redisConnectionTimeout = 3 * time.Second
)

type MockMarketViewer struct {
	Markets []models.Market
	Err     error
}

func (m *MockMarketViewer) ViewMarkets(_ context.Context, _ []models.UserRole) ([]models.Market, error) {
	if m.Err != nil {
		return nil, m.Err
	}

	return m.Markets, nil
}

type Suite struct {
	Test         *testing.T
	OrderClient  proto.OrderServiceClient
	Pool         *pgxpool.Pool
	MarketViewer *MockMarketViewer
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

	connection, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		test.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connection)
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
		test.Fatalf("failed to start cache container: %v", err)
	}
	test.Cleanup(func() {
		if err := redisC.Terminate(context.Background()); err != nil {
			test.Logf("failed to terminate cache container: %v", err)
		}
	})

	redisHost, err := redisC.Host(ctx)
	if err != nil {
		test.Fatalf("failed to get cache host: %v", err)
	}
	redisPort, err := redisC.MappedPort(ctx, "6379/tcp")
	if err != nil {
		test.Fatalf("failed to get cache port: %v", err)
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
	rateLimiter := repoRedis.NewOrderRateLimiter(redisClient, createRateLimit, createRateWindow)

	marketViewer := &MockMarketViewer{
		Markets: []models.Market{},
	}

	orderRepo := repoPostgres.NewOrderStore(pool)
	orderSvc := svcOrder.NewService(orderRepo, orderRepo, marketViewer, rateLimiter, defaultCreateTimeout)

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
	grpcOrder.Register(grpcServer, orderSvc)

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
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		test.Fatalf("failed to connect to grpc server: %v", err)
	}
	test.Cleanup(func() {
		_ = conn.Close()
	})

	return ctx, &Suite{
		Test:         test,
		OrderClient:  proto.NewOrderServiceClient(conn),
		Pool:         pool,
		MarketViewer: marketViewer,
	}
}

func (s *Suite) SetAvailableMarkets(markets ...models.Market) {
	s.MarketViewer.Markets = markets
	s.MarketViewer.Err = nil
}

func (s *Suite) SetMarketViewerError(err error) {
	s.MarketViewer.Err = err
}

func (s *Suite) ClearOrders(ctx context.Context) {
	s.Test.Helper()

	if _, err := s.Pool.Exec(ctx, "DELETE FROM orders"); err != nil {
		s.Test.Fatalf("failed to clear orders: %v", err)
	}
}

func (s *Suite) CountOrders(ctx context.Context) int {
	s.Test.Helper()

	var count int
	if err := s.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&count); err != nil {
		s.Test.Fatalf("failed to count orders: %v", err)
	}

	return count
}

func (s *Suite) OrderExistsInDB(ctx context.Context, orderID string) bool {
	s.Test.Helper()

	var exists bool
	err := s.Pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM orders WHERE id = $1)", orderID,
	).Scan(&exists)

	if err != nil {
		s.Test.Fatalf("failed to check order existence: %v", err)
	}

	return exists
}

func NewMarket() models.Market {
	return models.Market{
		ID:        uuid.New(),
		Name:      "BTC-USDT",
		Enabled:   true,
		DeletedAt: nil,
	}
}

func ValidCreateRequest(userID, marketID string) *proto.CreateOrderRequest {
	return &proto.CreateOrderRequest{
		UserId:    userID,
		MarketId:  marketID,
		OrderType: proto.OrderType_TYPE_LIMIT,
		Price:     &decimal.Decimal{Value: "100.50"},
		Quantity:  10,
	}
}
