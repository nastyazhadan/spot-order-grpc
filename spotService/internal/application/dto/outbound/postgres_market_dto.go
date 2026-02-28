package outbound

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type Market struct {
	ID        uuid.UUID  `db:"id"`
	Name      string     `db:"name"`
	Enabled   bool       `db:"enabled"`
	DeletedAt *time.Time `db:"deleted_at"`
}

func (m Market) MarketToDomain() models.Market {
	return models.Market{
		ID:        m.ID,
		Name:      m.Name,
		Enabled:   m.Enabled,
		DeletedAt: m.DeletedAt,
	}
}
