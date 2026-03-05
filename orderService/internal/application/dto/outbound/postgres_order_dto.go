package outbound

import (
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
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

func (o Order) ToDomain() models.Order {
	return models.Order{
		ID:        o.ID,
		UserID:    o.UserID,
		MarketID:  o.MarketID,
		Type:      models.OrderType(o.Type),
		Price:     sharedModels.NewDecimal(o.Price),
		Quantity:  o.Quantity,
		Status:    models.OrderStatus(o.Status),
		CreatedAt: o.CreatedAt,
	}
}

func FromDomain(order models.Order) Order {
	return Order{
		ID:        order.ID,
		UserID:    order.UserID,
		MarketID:  order.MarketID,
		Type:      int16(order.Type),
		Price:     order.Price.Value(),
		Quantity:  order.Quantity,
		Status:    int16(order.Status),
		CreatedAt: order.CreatedAt,
	}
}
