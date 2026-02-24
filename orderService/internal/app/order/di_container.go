package order

import (
	"context"
	"sync"
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

	orderRepository     *repoOrder.OrderStore
	orderRepositoryOnce sync.Once

	orderService     grpcOrder.Order
	orderServiceOnce sync.Once
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

func (d *DiContainer) OrderRepository(_ context.Context) *repoOrder.OrderStore {
	d.orderRepositoryOnce.Do(func() {
		d.orderRepository = repoOrder.NewOrderStore(d.dbPool)
	})

	return d.orderRepository
}

func (d *DiContainer) OrderService(ctx context.Context) grpcOrder.Order {
	d.orderServiceOnce.Do(func() {
		store := d.OrderRepository(ctx)
		d.orderService = svcOrder.NewService(store, store, d.marketViewer, d.createTimeout)
	})

	return d.orderService
}
