package mapper

import (
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"

	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"
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
