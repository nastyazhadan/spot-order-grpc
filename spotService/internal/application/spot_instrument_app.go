package application

import (
	"context"

	"go.uber.org/fx"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/spot"
)

func Run(ctx context.Context, cfg config.SpotConfig) {
	app := fx.New(
		fx.Supply(
			fx.Annotate(
				ctx,
				fx.As(new(context.Context)),
				fx.ResultTags(`name:"app_ctx"`),
			),
			cfg,
		),

		spot.InfraProviders,
		spot.ServiceProviders,
		spot.Lifecycle,
		spot.GRPCProviders,
	)

	app.Run()
}
