package inbound

import (
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
)

func TypeFromProto(orderType proto.OrderType) models.OrderType {
	switch orderType {
	case proto.OrderType_TYPE_LIMIT:
		return models.OrderTypeLimit
	case proto.OrderType_TYPE_MARKET:
		return models.OrderTypeMarket
	case proto.OrderType_TYPE_STOP_LOSS:
		return models.OrderTypeStopLoss
	case proto.OrderType_TYPE_TAKE_PROFIT:
		return models.OrderTypeTakeProfit
	default:
		return models.OrderTypeUnspecified
	}
}

func StatusToProto(orderStatus models.OrderStatus) proto.OrderStatus {
	switch orderStatus {
	case models.OrderStatusCreated:
		return proto.OrderStatus_STATUS_CREATED
	case models.OrderStatusPending:
		return proto.OrderStatus_STATUS_PENDING
	case models.OrderStatusFilled:
		return proto.OrderStatus_STATUS_FILLED
	case models.OrderStatusCancelled:
		return proto.OrderStatus_STATUS_CANCELLED
	default:
		return proto.OrderStatus_STATUS_UNSPECIFIED
	}
}
