package mapper

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"
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
