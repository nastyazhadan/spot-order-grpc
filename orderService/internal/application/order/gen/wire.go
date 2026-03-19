//go:build wireinject

package gen

import (
	"context"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/auth"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type Container struct {
	OrderService *order.OrderService
	AuthService  *auth.AuthService
	RedisClient  *redis.Client
	PostgresPool *pgxpool.Pool
}

func NewContainer(
	ctx context.Context,
	marketViewer order.MarketViewer,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) (*Container, error) {
	wire.Build(
		providePostgresPool,
		provideRedisClient,
		provideCacheStore,
		provideOrderStore,
		provideRateLimiters,
		provideJWTManager,
		provideRefreshTokenStore,
		provideAuthService,
		provideOrderServiceConfig,
		provideOrderService,
		wire.Struct(new(Container), "*"),
	)
	return nil, nil
}
