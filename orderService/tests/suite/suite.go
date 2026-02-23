//go:build integration

package suite

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

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
	repoOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/postgres"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	migrate "github.com/nastyazhadan/spot-order-grpc/shared/infra/db/migrator"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"
)

const (
	dbUser     = "test_user"
	dbPassword = "test_password"
	dbName     = "order_test_db"

	DefaultCreateTimeout = 5 * time.Second
	LongTimeout          = 2 * time.Minute
	StartupTimeout       = 30 * time.Second
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

	ctx, cancel := context.WithTimeout(context.Background(), LongTimeout)
	test.Cleanup(cancel)

	container, err := pgContainer.Run(ctx,
		"postgres:17.0-alpine3.20",
		pgContainer.WithDatabase(dbName),
		pgContainer.WithUsername(dbUser),
		pgContainer.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(StartupTimeout),
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

	marketViewer := &MockMarketViewer{
		Markets: []models.Market{},
	}

	orderRepo := repoOrder.NewOrderStore(pool)
	orderSvc := svcOrder.NewService(orderRepo, orderRepo, marketViewer, DefaultCreateTimeout)

	grpcServer := grpc.NewServer()
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
