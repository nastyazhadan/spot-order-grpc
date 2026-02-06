package models

import (
	"time"

	spot_orderv1 "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/genproto/googleapis/type/decimal"
)

type Order struct {
	ID        string
	UserID    string
	MarketID  string
	Type      spot_orderv1.OrderType
	Price     *decimal.Decimal
	Quantity  int64
	Status    spot_orderv1.OrderStatus
	CreatedAt time.Time
}
