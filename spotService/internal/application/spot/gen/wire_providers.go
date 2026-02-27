package gen

import (
	"context"
	"fmt"
	"time"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
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

func provideRedisPool(cfg config.SpotConfig) *redigo.Pool {
	return &redigo.Pool{
		MaxIdle:     cfg.Redis.MaxIdle,
		IdleTimeout: cfg.Redis.IdleTimeout,
		DialContext: func(ctx context.Context) (redigo.Conn, error) {
			return redigo.DialContext(ctx, "tcp", cfg.Redis.Address())
		},
	}
}

func provideRedisClient(pool *redigo.Pool, cfg config.SpotConfig) cache.Client {
	return cache.NewClient(pool, zapLogger.Logger(), cfg.Redis.ConnectionTimeout)
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
