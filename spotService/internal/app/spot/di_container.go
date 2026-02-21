package spot

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	repoSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
)

type DiContainer struct {
	dbPool *pgxpool.Pool

	spotRepository svcSpot.MarketRepository
	spotService    grpcSpot.SpotInstrument
}

func NewDIContainer(pool *pgxpool.Pool) *DiContainer {
	return &DiContainer{
		dbPool: pool,
	}
}

func (container *DiContainer) SpotRepository(_ context.Context) svcSpot.MarketRepository {
	if container.spotRepository == nil {
		container.spotRepository = repoSpot.NewMarketStore(container.dbPool)
	}

	return container.spotRepository
}

func (container *DiContainer) SpotService(ctx context.Context) grpcSpot.SpotInstrument {
	if container.spotService == nil {
		container.spotService = svcSpot.NewService(container.SpotRepository(ctx))
	}

	return container.spotService
}
