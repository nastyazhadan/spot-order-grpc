package mapper

import (
	"fmt"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func MarketFromProto(market *proto.Market) (models.Market, error) {
	var deletedAt *time.Time
	if ts := market.GetDeletedAt(); ts != nil {
		t := ts.AsTime()
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
