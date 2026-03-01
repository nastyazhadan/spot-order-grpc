package mapper

import (
	"fmt"
	"time"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/google/uuid"
)

func MarketFromProto(market *proto.Market) (models.Market, error) {
	var deletedAt *time.Time
	if delTime := market.GetDeletedAt(); delTime != nil {
		t := delTime.AsTime()
		deletedAt = &t
	}

	id, err := uuid.Parse(market.GetId())
	if err != nil {
		return models.Market{}, fmt.Errorf("invalid market id %q: %w", market.GetId(), err)
	}

	return models.Market{
		ID:        id,
		Name:      market.GetName(),
		Enabled:   market.GetEnabled(),
		DeletedAt: deletedAt,
	}, nil
}
