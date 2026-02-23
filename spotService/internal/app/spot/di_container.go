package spot

import (
	"context"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/redis"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/redis"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
)

type DiContainer struct {
	dbPool *pgxpool.Pool

	spotRepository      svcSpot.MarketRepository
	spotCacheRepository svcSpot.MarketCacheRepository

	spotService grpcSpot.SpotInstrument

	redisPool   *redigo.Pool
	redisClient redis.RedisClient
	redisConfig config.RedisConfig
}

func NewDIContainer(pool *pgxpool.Pool, redisCfg config.RedisConfig) *DiContainer {
	return &DiContainer{
		dbPool:      pool,
		redisConfig: redisCfg,
	}
}

func (d *DiContainer) SpotRepository(_ context.Context) svcSpot.MarketRepository {
	if d.spotRepository == nil {
		d.spotRepository = repoPostgres.NewMarketStore(d.dbPool)
	}

	return d.spotRepository
}

func (d *DiContainer) SpotCacheRepository() svcSpot.MarketCacheRepository {
	if d.spotCacheRepository == nil {
		d.spotCacheRepository = repoRedis.NewMarketCacheRepository(d.RedisClient())
	}

	return d.spotCacheRepository
}

func (d *DiContainer) SpotService(ctx context.Context) grpcSpot.SpotInstrument {
	if d.spotService == nil {
		d.spotService = svcSpot.NewService(
			d.SpotRepository(ctx),
			d.SpotCacheRepository(),
			d.redisConfig.CacheTTL,
		)
	}

	return d.spotService
}

func (d *DiContainer) RedisPool() *redigo.Pool {
	if d.redisPool == nil {
		d.redisPool = &redigo.Pool{
			MaxIdle:     d.redisConfig.MaxIdle,
			IdleTimeout: d.redisConfig.IdleTimeout,
			DialContext: func(ctx context.Context) (redigo.Conn, error) {
				return redigo.DialContext(ctx, "tcp", d.redisConfig.Address())
			},
		}
	}

	return d.redisPool
}

func (d *DiContainer) RedisClient() redis.RedisClient {
	if d.redisClient == nil {
		d.redisClient = redis.NewClient(
			d.RedisPool(),
			zapLogger.Logger(),
			d.redisConfig.ConnectionTimeout,
		)
	}

	return d.redisClient
}
