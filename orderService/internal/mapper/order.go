package mapper

import (
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"
)

func TypeFromProto(orderType proto.OrderType) models.Type {
	switch orderType {
	case proto.OrderType_TYPE_LIMIT:
		return models.OrderTypeLimit
	default:
		return models.OrderTypeUnspecified
	}
}

func StatusToProto(orderStatus models.OrderStatus) proto.OrderStatus {
	switch orderStatus {
	case models.OrderStatusCreated:
		return proto.OrderStatus_STATUS_CREATED
	case models.OrderStatusCancelled:
		return proto.OrderStatus_STATUS_CANCELLED
	default:
		return proto.OrderStatus_STATUS_UNSPECIFIED
	}
}
