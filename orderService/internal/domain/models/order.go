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
	Type      Type
	Price     Decimal
	Quantity  int64
	Status    Status
	CreatedAt time.Time
}

type Type uint8

const (
	TypeUnspecified Type = iota
	TypeLimit
)

type Status uint8

const (
	StatusUnspecified Status = iota
	StatusCreated
	StatusCancelled
)

type Decimal *decimal.Decimal
