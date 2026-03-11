package gen

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
)

func providePostgresPool(ctx context.Context, cfg config.SpotConfig) (*pgxpool.Pool, error) {
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

func provideRedisClient(cfg config.SpotConfig) (*redis.Client, error) {
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

func provideMarketStore(pool *pgxpool.Pool) *repoPostgres.MarketStore {
	return repoPostgres.NewMarketStore(pool)
}

func provideMarketCacheRepository(client cache.Client) *repoRedis.MarketCacheRepository {
	return repoRedis.NewMarketCacheRepository(client)
}

func provideSpotService(
	repository *repoPostgres.MarketStore,
	cacheRepository *repoRedis.MarketCacheRepository,
	cfg config.SpotConfig,
) *svcSpot.Service {
	return svcSpot.NewService(repository, cacheRepository, cfg.Redis.CacheTTL, cfg.LoadMarketsTimeout)
}
