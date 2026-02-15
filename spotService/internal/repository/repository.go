package repository

import (
	"context"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketRepository interface {
	ListAll(ctx context.Context) ([]models.Market, error)
}
