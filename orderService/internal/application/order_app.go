package application

import (
	"context"

	"go.uber.org/fx"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

func Run(ctx context.Context, cfg config.OrderConfig) {
	app := fx.New(
		fx.Supply(
			fx.Annotate(
				ctx,
				fx.As(new(context.Context)),
				fx.ResultTags(`name:"app_ctx"`),
			),
			cfg,
		),

		order.InfraProviders,
		order.ServiceProviders,
		order.GRPCProviders,
		order.Lifecycle,
	)

	app.Run()
}
