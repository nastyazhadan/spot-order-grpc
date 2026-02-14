package mapper

import (
	"spotOrder/internal/domain/models"

	proto "github.com/nastyazhadan/protos/gen/go/spot_order"
)

func TypeFromProto(orderType proto.OrderType) models.Type {
	switch orderType {
	case proto.OrderType_TYPE_LIMIT:
		return models.TypeLimit
	default:
		return models.TypeUnspecified
	}
}

func StatusToProto(orderStatus models.Status) proto.OrderStatus {
	switch orderStatus {
	case models.StatusCreated:
		return proto.OrderStatus_STATUS_CREATED
	case models.StatusCancelled:
		return proto.OrderStatus_STATUS_CANCELLED
	default:
		return proto.OrderStatus_STATUS_UNSPECIFIED
	}
}
