package mapper

import (
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

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
