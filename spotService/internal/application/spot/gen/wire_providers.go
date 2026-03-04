package gen

import (
	"context"
	"fmt"
	"time"

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

type CacheTTL time.Duration

func providePostgresPool(ctx context.Context, cfg config.SpotConfig) (*pgxpool.Pool, error) {
	pool, err := db.SetupDB(ctx, cfg.DBURI, migrations.Migrations)
	if err != nil {
		return nil, fmt.Errorf("postgres.SetupDB: %w", err)
	}

	return pool, nil
}

func provideRedisClient(cfg config.SpotConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:            cfg.Redis.Address(),
		DialTimeout:     cfg.Redis.ConnectionTimeout,
		ReadTimeout:     cfg.Redis.ConnectionTimeout,
		WriteTimeout:    cfg.Redis.ConnectionTimeout,
		PoolSize:        cfg.Redis.MaxIdle,
		ConnMaxIdleTime: cfg.Redis.IdleTimeout,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
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

func provideCacheTTL(cfg config.SpotConfig) CacheTTL {
	return CacheTTL(cfg.Redis.CacheTTL)
}

func provideSpotService(
	repository *repoPostgres.MarketStore,
	cacheRepository *repoRedis.MarketCacheRepository,
	cacheTTL CacheTTL,
) *svcSpot.Service {
	return svcSpot.NewService(repository, cacheRepository, time.Duration(cacheTTL))
}
