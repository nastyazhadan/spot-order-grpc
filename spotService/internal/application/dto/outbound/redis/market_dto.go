package redis

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketRedisView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	DeletedAtNs *int64 `json:"deleted_at,omitempty"`
	UpdatedAtNs *int64 `json:"updated_at,omitempty"`
}

func (m MarketRedisView) ToDomain() (models.Market, error) {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return models.Market{}, err
	}

	var deletedAt *time.Time
	if m.DeletedAtNs != nil {
		t := time.Unix(0, *m.DeletedAtNs).UTC()
		deletedAt = &t
	}

	var updatedAt time.Time
	if m.UpdatedAtNs != nil {
		updatedAt = time.Unix(0, *m.UpdatedAtNs).UTC()
	}

	return models.Market{
		ID:        id,
		Name:      m.Name,
		Enabled:   m.Enabled,
		DeletedAt: deletedAt,
		UpdatedAt: updatedAt,
	}, nil
}

func FromDomain(market models.Market) MarketRedisView {
	var deletedAtNs *int64
	if market.DeletedAt != nil {
		nanoTime := market.DeletedAt.UTC().UnixNano()
		deletedAtNs = &nanoTime
	}

	var updatedAtNs *int64
	if !market.UpdatedAt.IsZero() {
		nanoTime := market.UpdatedAt.UTC().UnixNano()
		updatedAtNs = &nanoTime
	}

	return MarketRedisView{
		ID:          market.ID.String(),
		Name:        market.Name,
		Enabled:     market.Enabled,
		DeletedAtNs: deletedAtNs,
		UpdatedAtNs: updatedAtNs,
	}
}
