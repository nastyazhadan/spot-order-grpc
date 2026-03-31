package models

import (
	"time"

	"github.com/google/uuid"
)

const MarketStateChangedEventType = "market.state.changed"

type MarketStateChangedEvent struct {
	EventID   uuid.UUID
	MarketID  uuid.UUID
	Enabled   bool
	DeletedAt *time.Time
	UpdatedAt time.Time
}
