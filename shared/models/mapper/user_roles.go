package mapper

import (
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"
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
