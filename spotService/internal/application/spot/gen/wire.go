//go:build wireinject

package gen

import (
	"context"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
)

type Container struct {
	SpotService  *svcSpot.MarketViewer
	RedisClient  *redis.Client
	PostgresPool *pgxpool.Pool
}

func NewContainer(
	ctx context.Context,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) (*Container, error) {
	wire.Build(
		providePostgresPool,
		provideRedisClient,
		provideCacheStore,
		provideMarketStore,
		provideMarketCacheRepository,
		provideSpotService,
		wire.Struct(new(Container), "*"),
	)
	return nil, nil
}
