package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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

type Decimal struct {
	value decimal.Decimal
}

func NewDecimal(raw string) (Decimal, error) {
	raw = strings.TrimSpace(raw)

	v, err := decimal.NewFromString(raw)
	if err != nil {
		return Decimal{}, err
	}

	return Decimal{value: v}, nil
}

func (d Decimal) String() string {
	return d.value.String()
}

func (d Decimal) IsPositive() bool {
	return d.value.IsPositive()
}
