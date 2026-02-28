package inbound

import (
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func UserRoleFromProto(userRole proto.UserRole) models.UserRole {
	switch userRole {
	case proto.UserRole_ROLE_USER:
		return models.UserRoleUser
	case proto.UserRole_ROLE_ADMIN:
		return models.UserRoleAdmin
	case proto.UserRole_ROLE_VIEWER:
		return models.UserRoleViewer
	default:
		return models.UserRoleUnspecified
	}
}
