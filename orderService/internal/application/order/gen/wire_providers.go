package gen

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	repoPostgres "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
)

type RateLimiters struct {
	Create svcOrder.RateLimiter
	Get    svcOrder.RateLimiter
}

func providePostgresPool(ctx context.Context, cfg config.OrderConfig) (*pgxpool.Pool, error) {
	pool, err := db.SetupDBWithPoolConfig(ctx, cfg.DBURI, migrations.Migrations, db.PoolConfig{
		MaxConnections:  cfg.PostgresPool.MaxConnections,
		MinConnections:  cfg.PostgresPool.MinConnections,
		MaxConnLifetime: cfg.PostgresPool.MaxConnLifetime,
		MaxConnIdleTime: cfg.PostgresPool.MaxConnIdleTime,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres.SetupDB: %w", err)
	}

	return pool, nil
}

func provideRedisClient(cfg config.OrderConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Address(),

		DialTimeout:  cfg.Redis.ConnectionTimeout,
		ReadTimeout:  cfg.Redis.ConnectionTimeout,
		WriteTimeout: cfg.Redis.ConnectionTimeout,

		PoolSize:       cfg.Redis.PoolSize,
		MinIdleConns:   cfg.Redis.MinIdle,
		MaxIdleConns:   cfg.Redis.MaxIdle,
		MaxActiveConns: cfg.Redis.MaxActiveConns,

		ConnMaxIdleTime: cfg.Redis.IdleTimeout,
		ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
	})

	pingCtx, cancel := context.WithTimeout(context.Background(), cfg.Redis.ConnectionTimeout)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("redis.Ping: %w", err)
	}

	return client, nil
}

func provideCacheClient(client *redis.Client) cache.Client {
	return cache.NewClient(client)
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

func provideOrderService(
	store *repoPostgres.OrderStore,
	marketViewer svcOrder.MarketViewer,
	rateLimiter RateLimiters,
	cfg config.OrderConfig,
) *svcOrder.OrderService {
	return svcOrder.NewOrderService(
		store,
		store,
		marketViewer,
		rateLimiter.Create,
		rateLimiter.Get,
		cfg.CreateTimeout,
	)
}
