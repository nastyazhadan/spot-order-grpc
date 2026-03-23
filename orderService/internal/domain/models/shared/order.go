package shared

import (
	"strings"

	"github.com/shopspring/decimal"
)

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

func (s OrderStatus) String() string {
	switch s {
	case OrderStatusCreated:
		return "created"
	case OrderStatusPending:
		return "pending"
	case OrderStatusFilled:
		return "filled"
	case OrderStatusCancelled:
		return "cancelled"
	default:
		return "unspecified"
	}
}

func (t OrderType) String() string {
	switch t {
	case OrderTypeLimit:
		return "limit"
	case OrderTypeMarket:
		return "market"
	case OrderTypeStopLoss:
		return "stop_loss"
	case OrderTypeTakeProfit:
		return "take_profit"
	default:
		return "unspecified"
	}
}

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
