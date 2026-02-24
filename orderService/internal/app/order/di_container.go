package order

import (
	"context"
	"sync"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/jackc/pgx/v5/pgxpool"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	repoOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/redis"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/redis"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
)

type DiContainer struct {
	dbPool       *pgxpool.Pool
	marketViewer svcOrder.MarketViewer
	orderConfig  config.OrderConfig

	orderRepository     *repoOrder.OrderStore
	orderRepositoryOnce sync.Once

	orderService     grpcOrder.Order
	orderServiceOnce sync.Once

	redisPool     *redigo.Pool
	redisPoolOnce sync.Once

	redisClient     redis.RedisClient
	redisClientOnce sync.Once

	rateLimiter     svcOrder.RateLimiter
	rateLimiterOnce sync.Once
}

func NewDIContainer(
	dbPool *pgxpool.Pool,
	marketViewer svcOrder.MarketViewer,
	orderConfig config.OrderConfig,
) *DiContainer {
	if dbPool == nil {
		panic("dbPool is nil")
	}

	if marketViewer == nil {
		panic("marketViewer is nil")
	}

	return &DiContainer{
		dbPool:       dbPool,
		marketViewer: marketViewer,
		orderConfig:  orderConfig,
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
		d.orderService = svcOrder.NewService(
			store,
			store,
			d.marketViewer,
			d.RateLimiter(),
			d.orderConfig.CreateTimeout,
		)
	})

	return d.orderService
}

func (d *DiContainer) RateLimiter() svcOrder.RateLimiter {
	d.rateLimiterOnce.Do(func() {
		d.rateLimiter = repoRedis.NewOrderRateLimiter(
			d.RedisClient(),
			d.orderConfig.RateLimiter.Orders,
			d.orderConfig.RateLimiter.Window,
		)
	})

	return d.rateLimiter
}

func (d *DiContainer) RedisPool() *redigo.Pool {
	d.redisPoolOnce.Do(func() {
		d.redisPool = &redigo.Pool{
			MaxIdle:     d.orderConfig.Redis.MaxIdle,
			IdleTimeout: d.orderConfig.Redis.IdleTimeout,
			DialContext: func(ctx context.Context) (redigo.Conn, error) {
				return redigo.DialContext(ctx, "tcp", d.orderConfig.Redis.Address())
			},
		}
	})

	return d.redisPool
}

func (d *DiContainer) RedisClient() redis.RedisClient {
	d.redisClientOnce.Do(func() {
		d.redisClient = redis.NewClient(
			d.RedisPool(),
			zapLogger.Logger(),
			d.orderConfig.Redis.ConnectionTimeout,
		)
	})

	return d.redisClient
}
