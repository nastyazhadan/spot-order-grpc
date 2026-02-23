package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketRedisView struct {
	ID          string `redis:"id"          json:"id"`
	Name        string `redis:"name"        json:"name"`
	Enabled     bool   `redis:"enabled"     json:"enabled"`
	DeletedAtNs *int64 `redis:"deleted_at"  json:"deleted_at,omitempty"`
}

func (m MarketRedisView) ToDomainView() (models.Market, error) {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return models.Market{}, err
	}

	var deletedAt *time.Time
	if m.DeletedAtNs != nil {
		t := time.Unix(0, *m.DeletedAtNs)
		deletedAt = &t
	}

	return models.Market{
		ID:        id,
		Name:      m.Name,
		Enabled:   m.Enabled,
		DeletedAt: deletedAt,
	}, nil
}
