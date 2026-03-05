package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type Order struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	MarketID  uuid.UUID
	Type      OrderType
	Price     models.Decimal
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
