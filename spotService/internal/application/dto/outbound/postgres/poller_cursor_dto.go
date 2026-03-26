package postgres

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
)

type PollerCursor struct {
	PollerName string
	LastSeenAt time.Time
	LastSeenID uuid.UUID
}

func ToDomain(cursor PollerCursor) models.PollerCursor {
	return models.PollerCursor{
		PollerName: cursor.PollerName,
		LastSeenAt: cursor.LastSeenAt,
		LastSeenID: cursor.LastSeenID,
	}
}

func FromDomain(cursor models.PollerCursor) PollerCursor {
	return PollerCursor{
		PollerName: cursor.PollerName,
		LastSeenAt: cursor.LastSeenAt,
		LastSeenID: cursor.LastSeenID,
	}
}
