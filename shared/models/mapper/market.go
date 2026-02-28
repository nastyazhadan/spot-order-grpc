package mapper

import (
	"fmt"
	"time"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func MarketToProto(market models.Market) *proto.Market {
	var deletedAt *timestamppb.Timestamp
	if market.DeletedAt != nil {
		deletedAt = timestamppb.New(*market.DeletedAt)
	}

	return &proto.Market{
		Id:        market.ID.String(),
		Name:      market.Name,
		Enabled:   market.Enabled,
		DeletedAt: deletedAt,
	}
}
