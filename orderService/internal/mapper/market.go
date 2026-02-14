package mapper

import (
	"spotOrder/internal/domain/models"
	"time"

	proto "github.com/nastyazhadan/protos/gen/go/spot_order"
)

func MarketFromProto(market *proto.Market) models.Market {
	var deletedAt *time.Time
	if ts := market.GetDeletedAt(); ts != nil {
		t := ts.AsTime()
		deletedAt = &t
	}

	return models.Market{
		ID:        market.GetId(),
		Enabled:   market.GetEnabled(),
		DeletedAt: deletedAt,
	}
}
