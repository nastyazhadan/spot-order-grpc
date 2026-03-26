package models

import (
	"time"

	"github.com/google/uuid"
)

type PollerCursor struct {
	PollerName string
	LastSeenAt time.Time
	LastSeenID uuid.UUID
}
