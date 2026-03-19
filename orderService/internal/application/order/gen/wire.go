//go:build wireinject

package gen

import (
	"context"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type Container struct {
	OrderService *svcOrder.OrderService
	RedisClient  *redis.Client
	PostgresPool *pgxpool.Pool
}

func NewContainer(
	ctx context.Context,
	marketViewer svcOrder.MarketViewer,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) (*Container, error) {
	wire.Build(
		providePostgresPool,
		provideRedisClient,
		provideCacheStore,
		provideOrderStore,
		provideRateLimiters,
		provideOrderServiceConfig,
		provideOrderService,
		wire.Struct(new(Container), "*"),
	)
	return nil, nil
}
