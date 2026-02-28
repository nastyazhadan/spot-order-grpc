package inbound

import (
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"google.golang.org/protobuf/types/known/timestamppb"
)

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
