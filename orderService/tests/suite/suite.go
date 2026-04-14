//go:build integration

package suite

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	pgContainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/order"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/outbox"
	repoRedisIdem "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/idempotency"
	repoRedisMarket "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/market"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/order"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/producer"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	protoCommon "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	migrate "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db/migrator"
	grpcErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

const (
	dbUser     = "test_user"
	dbPassword = "test_password"
	dbName     = "order_test_db"

	longTimeout    = 2 * time.Minute
	startupTimeout = 30 * time.Second

	testUserIDMetadataKey = "x-test-user-id"

	testServiceName    = "order-integration-test"
	testServiceTimeout = 5 * time.Second
	testBlockStoreTTL  = 10 * time.Minute
	testIdempotencyTTL = 24 * time.Hour

	createRateLimit   int64  = 1_000_000
	createRateWindow         = 1 * time.Hour
	createRedisPrefix string = "rate:order:create:"

	getRateLimit   int64  = 1_000_000
	getRateWindow         = 1 * time.Hour
	getRedisPrefix string = "rate:order:get:"
)

type MockMarketViewer struct {
	markets map[uuid.UUID]models.Market
	err     error
	mu      sync.RWMutex
}

func (m *MockMarketViewer) GetMarketByID(_ context.Context, id uuid.UUID) (models.Market, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return models.Market{}, m.err
	}
	market, ok := m.markets[id]
	if !ok {
		return models.Market{}, errors.ErrMarketNotFound{ID: id}
	}
	return market, nil
}

func (m *MockMarketViewer) setMarkets(markets []models.Market) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.err = nil
	m.markets = make(map[uuid.UUID]models.Market, len(markets))
	for _, market := range markets {
		m.markets[market.ID] = market
	}
}

func (m *MockMarketViewer) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.err = err
	m.markets = nil
}

type Suite struct {
	Test         *testing.T
	OrderClient  proto.OrderServiceClient
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	MarketViewer *MockMarketViewer
}

func New(test *testing.T) (context.Context, *Suite) {
	test.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), longTimeout)
	test.Cleanup(cancel)

	logger := zapLogger.NewNop()

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
		if err = container.Terminate(context.Background()); err != nil {
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
	if err = pool.Ping(ctx); err != nil {
		test.Fatalf("failed to ping postgres: %v", err)
	}
	test.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDBFromPool(pool)
	test.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err = migrate.NewMigrator(sqlDB, migrations.Migrations).Up(ctx); err != nil {
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
		if err = redisC.Terminate(context.Background()); err != nil {
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

	redisClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort.Port()),
	})
	if err = redisClient.Ping(ctx).Err(); err != nil {
		test.Fatalf("failed to ping redis: %v", err)
	}
	test.Cleanup(func() {
		_ = redisClient.Close()
	})

	orderCfg := config.OrderConfig{
		Service:  config.ServiceConfig{Name: testServiceName},
		Timeouts: config.TimeoutsConfig{Service: testServiceTimeout},
	}

	cacheStore := cache.New(redisClient)
	orderStore := repoPostgres.New(pool, orderCfg)
	outbox := outboxStore.New(pool, logger, orderCfg)
	eventProducer := producer.New(outbox, logger)

	createLimiter := repoRedis.NewOrderRateLimiter(cacheStore, createRateLimit, createRateWindow, createRedisPrefix)
	getLimiter := repoRedis.NewOrderRateLimiter(cacheStore, getRateLimit, getRateWindow, getRedisPrefix)
	blockStore := repoRedisMarket.New(cacheStore, testBlockStoreTTL, orderCfg)

	idempotencyStore := repoRedisIdem.New(cacheStore, testIdempotencyTTL)
	idempotencyAdapter := &redisIdemAdapter{store: idempotencyStore}
	idempotencyService := svcOrder.NewIdempotencyService(idempotencyAdapter, logger)

	marketViewer := &MockMarketViewer{
		markets: make(map[uuid.UUID]models.Market),
	}

	orderService := svcOrder.New(
		pool,
		orderStore,
		orderStore,
		marketViewer,
		blockStore,
		svcOrder.RateLimiters{Create: createLimiter, Get: getLimiter},
		svcOrder.Config{Timeout: testServiceTimeout, ServiceName: testServiceName},
		eventProducer,
		*idempotencyService,
		logger,
	)

	validator, err := validate.UnaryServerInterceptor()
	if err != nil {
		test.Fatalf("validate.UnaryServerInterceptor: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recovery.UnaryServerInterceptor(logger),
			testUserIDInjectorInterceptor,
			validator,
			grpcErrors.UnaryServerInterceptor(logger),
		),
	)
	grpcOrder.Register(grpcServer, orderService, logger)

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
		test.Fatalf("failed to connect to gRPC server: %v", err)
	}
	test.Cleanup(func() {
		_ = conn.Close()
	})

	return ctx, &Suite{
		Test:         test,
		OrderClient:  proto.NewOrderServiceClient(conn),
		Pool:         pool,
		Redis:        redisClient,
		MarketViewer: marketViewer,
	}
}

func (s *Suite) CtxWithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return metadata.NewOutgoingContext(ctx,
		metadata.Pairs(testUserIDMetadataKey, userID.String()),
	)
}

func (s *Suite) SetAvailableMarkets(markets ...models.Market) {
	s.MarketViewer.setMarkets(markets)
}

func (s *Suite) SetMarketViewerError(err error) {
	s.MarketViewer.setError(err)
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
	if err := s.Pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM orders WHERE id = $1)", orderID,
	).Scan(&exists); err != nil {
		s.Test.Fatalf("failed to check order existence: %v", err)
	}

	return exists
}

func (s *Suite) InsertOrderDirectly(ctx context.Context, orderID, userID, marketID uuid.UUID, dbStatus int) {
	s.Test.Helper()
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO orders (id, user_id, market_id, type, price, quantity, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		orderID, userID, marketID, 1, "100.00", 1, dbStatus, time.Now().UTC(),
	)
	if err != nil {
		s.Test.Fatalf("failed to insert order directly: %v", err)
	}
}

func NewMarket() models.Market {
	return models.Market{
		ID:        uuid.New(),
		Name:      "BTC-USDT",
		Enabled:   true,
		DeletedAt: nil,
	}
}

func ValidCreateRequest(marketID string) *proto.CreateOrderRequest {
	return &proto.CreateOrderRequest{
		MarketId:  marketID,
		OrderType: protoCommon.OrderType_TYPE_LIMIT,
		Price:     &decimal.Decimal{Value: "100.50"},
		Quantity:  10,
	}
}

func testUserIDInjectorInterceptor(
	ctx context.Context,
	request any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return handler(ctx, request)
	}

	values := md.Get(testUserIDMetadataKey)
	if len(values) == 0 {
		return handler(ctx, request)
	}

	userID, err := uuid.Parse(values[0])
	if err != nil {
		return handler(ctx, request)
	}

	if newCtx, ok := requestctx.ContextWithUserID(ctx, userID); ok {
		ctx = newCtx
	}

	return handler(ctx, request)
}

type redisIdemAdapter struct {
	store *repoRedisIdem.Store
}

func (a *redisIdemAdapter) Acquire(ctx context.Context, userID uuid.UUID, requestHash string) (svcOrder.IdempotencyResult, bool, error) {
	entry, acquired, err := a.store.Acquire(ctx, userID, requestHash)
	if err != nil {
		return svcOrder.IdempotencyResult{}, false, err
	}
	return svcOrder.IdempotencyResult{
		IsCompleted:  entry.IsCompleted(),
		IsProcessing: entry.IsProcessing(),
		OrderID:      entry.OrderID,
		OrderStatus:  entry.OrderStatus,
	}, acquired, nil
}

func (a *redisIdemAdapter) Complete(ctx context.Context, userID uuid.UUID, requestHash string, orderID uuid.UUID, orderStatus string) error {
	return a.store.Complete(ctx, userID, requestHash, orderID, orderStatus)
}

func (a *redisIdemAdapter) FailCleanup(ctx context.Context, userID uuid.UUID, requestHash string) error {
	return a.store.FailCleanup(ctx, userID, requestHash)
}
