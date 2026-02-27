//go:build wireinject

package gen

import (
	"context"

	redigo "github.com/gomodule/redigo/redis"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

type Container struct {
	OrderService grpcOrder.Order
	RedisPool    *redigo.Pool
	PostgresPool *pgxpool.Pool
}

func NewContainer(
	ctx context.Context,
	marketViewer svcOrder.MarketViewer,
	cfg config.OrderConfig,
) (*Container, error) {
	wire.Build(
		provideDBPool,
		provideRedisPool,
		provideRedisClient,
		provideOrderStore,
		provideRateLimiters,
		provideCreateTimeout,
		provideOrderService,
		wire.Bind(new(grpcOrder.Order), new(*svcOrder.Service)),
		wire.Struct(new(Container), "*"),
	)
	return nil, nil
}
