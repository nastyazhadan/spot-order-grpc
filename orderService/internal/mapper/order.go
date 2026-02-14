package mapper

import (
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"
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
