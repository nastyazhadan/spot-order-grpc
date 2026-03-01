package gen

import (
	"context"
	"fmt"
	"time"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/jackc/pgx/v5/pgxpool"

	repoPostgres "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
)

type RateLimiters struct {
	Create svcOrder.RateLimiter
	Get    svcOrder.RateLimiter
}

type CreateTimeout time.Duration

func provideDBPool(ctx context.Context, cfg config.OrderConfig) (*pgxpool.Pool, error) {
	pool, err := db.SetupDB(ctx, cfg.DBURI, migrations.Migrations)
	if err != nil {
		return nil, fmt.Errorf("postgres.SetupDB: %w", err)
	}

	return pool, nil
}

func provideRedisPool(cfg config.OrderConfig) *redigo.Pool {
	return &redigo.Pool{
		MaxIdle:     cfg.Redis.MaxIdle,
		IdleTimeout: cfg.Redis.IdleTimeout,
		DialContext: func(ctx context.Context) (redigo.Conn, error) {
			return redigo.DialContext(
				ctx,
				"tcp",
				cfg.Redis.Address(),
				redigo.DialConnectTimeout(cfg.Redis.ConnectionTimeout),
				redigo.DialReadTimeout(cfg.Redis.ConnectionTimeout),
				redigo.DialWriteTimeout(cfg.Redis.ConnectionTimeout),
			)
		},
	}
}

func provideRedisClient(pool *redigo.Pool, cfg config.OrderConfig) cache.Client {
	return cache.NewClient(pool, zapLogger.Logger(), cfg.Redis.ConnectionTimeout)
}

func provideOrderStore(pool *pgxpool.Pool) *repoPostgres.OrderStore {
	return repoPostgres.NewOrderStore(pool)
}

func provideRateLimiters(client cache.Client, cfg config.OrderConfig) RateLimiters {
	return RateLimiters{
		Create: repoRedis.NewOrderRateLimiter(
			client,
			cfg.RateLimiter.CreateOrder,
			cfg.RateLimiter.Window,
			"rate:order:create:",
		),
		Get: repoRedis.NewOrderRateLimiter(
			client,
			cfg.RateLimiter.GetOrderStatus,
			cfg.RateLimiter.Window,
			"rate:order:get:",
		),
	}
}

func provideCreateTimeout(cfg config.OrderConfig) CreateTimeout {
	return CreateTimeout(cfg.CreateTimeout)
}

func provideOrderService(
	store *repoPostgres.OrderStore,
	marketViewer svcOrder.MarketViewer,
	rateLimiter RateLimiters,
	timeout CreateTimeout,
) *svcOrder.Service {
	return svcOrder.NewService(
		store,
		store,
		marketViewer,
		rateLimiter.Create,
		rateLimiter.Get,
		time.Duration(timeout),
	)
}
