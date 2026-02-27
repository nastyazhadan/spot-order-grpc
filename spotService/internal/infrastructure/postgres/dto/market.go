package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis/dto"
)

type Market struct {
	ID        uuid.UUID  `db:"id"`
	Name      string     `db:"name"`
	Enabled   bool       `db:"enabled"`
	DeletedAt *time.Time `db:"deleted_at"`
}

func (m Market) ToDomain() models.Market {
	return models.Market{
		ID:        m.ID,
		Name:      m.Name,
		Enabled:   m.Enabled,
		DeletedAt: m.DeletedAt,
	}
}

func (m Market) ToRedisView() dto.MarketRedisView {
	var deletedAtNs *int64
	if m.DeletedAt != nil {
		nanoTime := m.DeletedAt.UnixNano()
		deletedAtNs = &nanoTime
	}

	return dto.MarketRedisView{
		ID:          m.ID.String(),
		Name:        m.Name,
		Enabled:     m.Enabled,
		DeletedAtNs: deletedAtNs,
	}
}
