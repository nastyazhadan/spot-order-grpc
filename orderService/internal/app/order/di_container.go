package order

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	repoOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/postgres"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
)

type DiContainer struct {
	dbPool        *pgxpool.Pool
	marketViewer  svcOrder.MarketViewer
	createTimeout time.Duration

	orderRepository *repoOrder.OrderStore
	orderService    grpcOrder.Order
}

func NewDIContainer(dbPool *pgxpool.Pool, marketViewer svcOrder.MarketViewer, createTimeout time.Duration) *DiContainer {
	if dbPool == nil {
		panic("dbPool is nil")
	}

	if marketViewer == nil {
		panic("marketViewer is nil")
	}

	return &DiContainer{
		dbPool:        dbPool,
		marketViewer:  marketViewer,
		createTimeout: createTimeout,
	}
}

func (c *DiContainer) OrderRepository(_ context.Context) *repoOrder.OrderStore {
	if c.orderRepository == nil {
		c.orderRepository = repoOrder.NewOrderStore(c.dbPool)
	}

	return c.orderRepository
}

func (c *DiContainer) OrderService(ctx context.Context) grpcOrder.Order {
	if c.orderService == nil {
		store := c.OrderRepository(ctx)
		c.orderService = svcOrder.NewService(store, store, c.marketViewer, c.createTimeout)
	}

	return c.orderService
}
