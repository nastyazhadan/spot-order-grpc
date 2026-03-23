package postgres

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
)

type Order struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	MarketID  uuid.UUID `db:"market_id"`
	Type      int16     `db:"type"`
	Price     string    `db:"price"`
	Quantity  int64     `db:"quantity"`
	Status    int16     `db:"status"`
	CreatedAt time.Time `db:"created_at"`
}

func (o Order) ToDomain() (models.Order, error) {
	price, err := shared.NewDecimal(o.Price)
	if err != nil {
		return models.Order{}, fmt.Errorf("invalid order price from db: %w", err)
	}

	return models.Order{
		ID:        o.ID,
		UserID:    o.UserID,
		MarketID:  o.MarketID,
		Type:      shared.OrderType(o.Type),
		Price:     price,
		Quantity:  o.Quantity,
		Status:    shared.OrderStatus(o.Status),
		CreatedAt: o.CreatedAt,
	}, nil
}

func FromDomain(order models.Order) Order {
	return Order{
		ID:        order.ID,
		UserID:    order.UserID,
		MarketID:  order.MarketID,
		Type:      int16(order.Type),
		Price:     order.Price.String(),
		Quantity:  order.Quantity,
		Status:    int16(order.Status),
		CreatedAt: order.CreatedAt,
	}
}
