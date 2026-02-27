//go:build wireinject

package gen

import (
	"context"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
)

type Container struct {
	SpotService  *svcSpot.Service
	RedisPool    *redigo.Pool
	PostgresPool *pgxpool.Pool
}

func NewContainer(ctx context.Context, cfg config.SpotConfig) (*Container, error) {
	wire.Build(
		providePostgresPool,
		provideRedisPool,
		provideRedisClient,
		provideMarketStore,
		provideMarketCacheRepository,
		provideCacheTTL,
		provideSpotService,
		wire.Struct(new(Container), "*"),
	)
	return nil, nil
}
