package mapper

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func MarketFromProto(market *proto.Market) (models.Market, error) {
	if market == nil {
		return models.Market{}, errors.New("proto market is nil")
	}
	var deletedAt *time.Time
	if delTime := market.GetDeletedAt(); delTime != nil {
		t := delTime.AsTime()
		deletedAt = &t
	}

	var updatedAt time.Time
	if protoUpdatedAt := market.GetUpdatedAt(); protoUpdatedAt != nil {
		updatedAt = protoUpdatedAt.AsTime().UTC()
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
		UpdatedAt: updatedAt,
	}, nil
}
