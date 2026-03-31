package inbound

import (
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
)

func TypeFromProto(orderType proto.OrderType) shared.OrderType {
	switch orderType {
	case proto.OrderType_TYPE_LIMIT:
		return shared.OrderTypeLimit
	case proto.OrderType_TYPE_MARKET:
		return shared.OrderTypeMarket
	case proto.OrderType_TYPE_STOP_LOSS:
		return shared.OrderTypeStopLoss
	case proto.OrderType_TYPE_TAKE_PROFIT:
		return shared.OrderTypeTakeProfit
	default:
		return shared.OrderTypeUnspecified
	}
}

func StatusToProto(orderStatus shared.OrderStatus) proto.OrderStatus {
	switch orderStatus {
	case shared.OrderStatusCreated:
		return proto.OrderStatus_STATUS_CREATED
	case shared.OrderStatusPending:
		return proto.OrderStatus_STATUS_PENDING
	case shared.OrderStatusFilled:
		return proto.OrderStatus_STATUS_FILLED
	case shared.OrderStatusCancelled:
		return proto.OrderStatus_STATUS_CANCELLED
	default:
		return proto.OrderStatus_STATUS_UNSPECIFIED
	}
}
