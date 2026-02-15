package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
)

type OrderRepository interface {
	SaveOrder(ctx context.Context, order models.Order) error
	GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error)
}
