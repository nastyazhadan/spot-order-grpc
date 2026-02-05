package mapper

import (
	"spotOrder/internal/domain/models"

	"github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func MapToProto(m models.Market) *spot_orderv1.Market {
	var deletedAt *timestamppb.Timestamp
	if m.DeletedAt != nil {
		deletedAt = timestamppb.New(*m.DeletedAt)
	}

	return &spot_orderv1.Market{
		Id:        m.ID,
		Enabled:   m.Enabled,
		DeletedAt: deletedAt,
	}
}
