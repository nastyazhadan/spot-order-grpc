package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"google.golang.org/genproto/googleapis/type/decimal"
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

func (order Order) ToDomain() models.Order {
	return models.Order{
		ID:        order.ID,
		UserID:    order.UserID,
		MarketID:  order.MarketID,
		Type:      models.OrderType(order.Type),
		Price:     &decimal.Decimal{Value: order.Price},
		Quantity:  order.Quantity,
		Status:    models.OrderStatus(order.Status),
		CreatedAt: order.CreatedAt,
	}
}

func FromDomain(order models.Order) Order {
	price := ""
	if order.Price != nil {
		price = order.Price.Value
	}

	return Order{
		ID:        order.ID,
		UserID:    order.UserID,
		MarketID:  order.MarketID,
		Type:      int16(order.Type),
		Price:     price,
		Quantity:  order.Quantity,
		Status:    int16(order.Status),
		CreatedAt: order.CreatedAt,
	}
}
