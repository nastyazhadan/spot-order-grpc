package models

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/type/decimal"
)

type Order struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	MarketID  uuid.UUID
	Type      OrderType
	Price     Decimal
	Quantity  int64
	Status    OrderStatus
	CreatedAt time.Time
}

type OrderType uint8

const (
	OrderTypeUnspecified OrderType = iota
	OrderTypeLimit
	OrderTypeMarket
	OrderTypeStopLoss
	OrderTypeTakeProfit
)

type OrderStatus uint8

const (
	OrderStatusUnspecified OrderStatus = iota
	OrderStatusCreated
	OrderStatusPending
	OrderStatusFilled
	OrderStatusCancelled
)

type Decimal *decimal.Decimal
