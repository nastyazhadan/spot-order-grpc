package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
)

type Order struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	MarketID  uuid.UUID
	Type      shared.OrderType
	Price     shared.Decimal
	Quantity  int64
	Status    shared.OrderStatus
	CreatedAt time.Time
}
