package spot

import (
	"context"
	"sync"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
)

type DiContainer struct {
	dbPool     *pgxpool.Pool
	spotConfig config.SpotConfig

	spotRepository     svcSpot.MarketRepository
	spotRepositoryOnce sync.Once

	spotCacheRepository     svcSpot.MarketCacheRepository
	spotCacheRepositoryOnce sync.Once

	spotService     grpcSpot.SpotInstrument
	spotServiceOnce sync.Once

	redisPool     *redigo.Pool
	redisPoolOnce sync.Once

	redisClient     cache.Client
	redisClientOnce sync.Once
}

func NewDIContainer(pool *pgxpool.Pool, spotCfg config.SpotConfig) *DiContainer {
	return &DiContainer{
		dbPool:     pool,
		spotConfig: spotCfg,
	}
}

func (d *DiContainer) SpotRepository(_ context.Context) svcSpot.MarketRepository {
	d.spotRepositoryOnce.Do(func() {
		d.spotRepository = repoPostgres.NewMarketStore(d.dbPool)
	})

	return d.spotRepository
}

func (d *DiContainer) SpotCacheRepository() svcSpot.MarketCacheRepository {
	d.spotCacheRepositoryOnce.Do(func() {
		d.spotCacheRepository = repoRedis.NewMarketCacheRepository(d.RedisClient())
	})

	return d.spotCacheRepository
}

func (d *DiContainer) SpotService(ctx context.Context) grpcSpot.SpotInstrument {
	d.spotServiceOnce.Do(func() {
		d.spotService = svcSpot.NewService(
			d.SpotRepository(ctx),
			d.SpotCacheRepository(),
			d.spotConfig.Redis.CacheTTL,
		)
	})

	return d.spotService
}

func (d *DiContainer) RedisPool() *redigo.Pool {
	d.redisPoolOnce.Do(func() {
		d.redisPool = &redigo.Pool{
			MaxIdle:     d.spotConfig.Redis.MaxIdle,
			IdleTimeout: d.spotConfig.Redis.IdleTimeout,
			DialContext: func(ctx context.Context) (redigo.Conn, error) {
				return redigo.DialContext(ctx, "tcp", d.spotConfig.Redis.Address())
			},
		}
	})

	return d.redisPool
}

func (d *DiContainer) RedisClient() cache.Client {
	d.redisClientOnce.Do(func() {
		d.redisClient = cache.NewClient(
			d.RedisPool(),
			zapLogger.Logger(),
			d.spotConfig.Redis.ConnectionTimeout,
		)
	})

	return d.redisClient
}
