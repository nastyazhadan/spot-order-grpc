package mapper

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	models2 "github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func MarketFromProto(market *proto.Market) (models2.Market, error) {
	var deletedAt *time.Time
	if delTime := market.GetDeletedAt(); delTime != nil {
		t := delTime.AsTime()
		deletedAt = &t
	}

	id, err := uuid.Parse(market.GetId())
	if err != nil {
		return models2.Market{}, fmt.Errorf("invalid market id %q: %w", market.GetId(), err)
	}

	return models2.Market{
		ID:        id,
		Name:      market.GetName(),
		Enabled:   market.GetEnabled(),
		DeletedAt: deletedAt,
	}, nil
}

func UserRoleToProto(userRole models.UserRole) proto.UserRole {
	switch userRole {
	case models.UserRoleUser:
		return proto.UserRole_ROLE_USER
	case models.UserRoleAdmin:
		return proto.UserRole_ROLE_ADMIN
	case models.UserRoleViewer:
		return proto.UserRole_ROLE_VIEWER
	default:
		return proto.UserRole_ROLE_UNSPECIFIED

	}
}
