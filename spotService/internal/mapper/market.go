package mapper

import (
	"spotOrder/internal/domain/models"

	proto "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func MarketToProto(market models.Market) *proto.Market {
	var deletedAt *timestamppb.Timestamp
	if market.DeletedAt != nil {
		deletedAt = timestamppb.New(*market.DeletedAt)
	}

	return &proto.Market{
		Id:        market.ID,
		Enabled:   market.Enabled,
		DeletedAt: deletedAt,
	}
}
