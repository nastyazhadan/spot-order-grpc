package models

import (
	"google.golang.org/genproto/googleapis/type/decimal"
)

type Decimal struct {
	Decimal *decimal.Decimal
}

func NewDecimal(value string) Decimal {
	return Decimal{
		Decimal: &decimal.Decimal{Value: value},
	}
}

func (d Decimal) Value() string {
	if d.Decimal == nil {
		return ""
	}

	return d.Decimal.Value
}
