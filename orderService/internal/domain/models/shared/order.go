package shared

import (
	"strings"

	"github.com/shopspring/decimal"
)

type OrderType uint16

const (
	OrderTypeUnspecified OrderType = iota
	OrderTypeLimit
	OrderTypeMarket
	OrderTypeStopLoss
	OrderTypeTakeProfit
)

type OrderStatus uint16

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

func (d Decimal) FitsNumeric(maxPrecision, maxScale int) bool {
	raw := d.value.String()
	raw = strings.TrimPrefix(raw, "-")

	parts := strings.SplitN(raw, ".", 2)

	integerDigits := countIntegerDigits(parts[0])
	fractionalDigits := 0

	if len(parts) == 2 {
		fractionalDigits = countFractionalDigits(parts[1])
	}

	maxIntegerDigits := maxPrecision - maxScale

	return integerDigits <= maxIntegerDigits && fractionalDigits <= maxScale
}

func countIntegerDigits(integerPart string) int {
	trimmed := strings.TrimLeft(integerPart, "0")
	if trimmed == "" {
		return 0
	}

	return len(trimmed)
}

func countFractionalDigits(fractionalPart string) int {
	trimmed := strings.TrimRight(fractionalPart, "0")
	return len(trimmed)
}
