//go:build wireinject

package gen

import (
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
}

func NewContainer(
	pool *pgxpool.Pool,
	marketViewer svcOrder.MarketViewer,
	cfg config.OrderConfig,
) *Container {
	wire.Build(
		provideRedisPool,
		provideRedisClient,
		provideOrderStore,
		provideRateLimiters,
		provideCreateTimeout,
		provideOrderService,
		wire.Bind(new(grpcOrder.Order), new(*svcOrder.Service)),
		wire.Struct(new(Container), "*"),
	)
	return nil
}
