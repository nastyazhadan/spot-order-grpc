package kafka

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	protoCommon "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
	protoEvent "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/events/v1"
)

func MarshalOrderCreated(event models.OrderCreatedEvent) ([]byte, error) {
	data := ToProtoOrderCreated(event)

	result, err := proto.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("proto.MarshalOrderCreated: %w", err)
	}

	return result, nil
}

func MarshalOrderStatusUpdated(event models.OrderStatusUpdatedEvent) ([]byte, error) {
	result := ToProtoOrderStatusUpdated(event)

	data, err := proto.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("proto.MarshalOrderStatusUpdated: %w", err)
	}

	return data, nil
}

func ToProtoOrderCreated(event models.OrderCreatedEvent) *protoEvent.OrderCreatedEvent {
	return &protoEvent.OrderCreatedEvent{
		EventId:   event.EventID.String(),
		OrderId:   event.OrderID.String(),
		UserId:    event.UserID.String(),
		MarketId:  event.MarketID.String(),
		OrderType: toProtoOrderType(event.Type),
		Price:     toProtoDecimal(event.Price),
		Quantity:  event.Quantity,
		Status:    toProtoOrderStatus(event.Status),
		CreatedAt: timestamppb.New(event.CreatedAt.UTC()),
	}
}

func ToProtoOrderStatusUpdated(event models.OrderStatusUpdatedEvent) *protoEvent.OrderStatusUpdatedEvent {
	return &protoEvent.OrderStatusUpdatedEvent{
		EventId:       event.EventID.String(),
		OrderId:       event.OrderID.String(),
		NewStatus:     toProtoOrderStatus(event.NewStatus),
		Reason:        event.Reason,
		CorrelationId: event.CorrelationID.String(),
		UpdatedAt:     timestamppb.New(event.UpdatedAt),
	}
}

// OrderCreatedKey - Использование order_id как ключа гарантирует, что все сообщения
// для одной заявки попадают в одну партицию (ordering per order)
func OrderCreatedKey(orderID uuid.UUID) []byte {
	return []byte(orderID.String())
}

func toProtoOrderStatus(status shared.OrderStatus) protoCommon.OrderStatus {
	switch status {
	case shared.OrderStatusCreated:
		return protoCommon.OrderStatus_STATUS_CREATED
	case shared.OrderStatusPending:
		return protoCommon.OrderStatus_STATUS_PENDING
	case shared.OrderStatusFilled:
		return protoCommon.OrderStatus_STATUS_FILLED
	case shared.OrderStatusCancelled:
		return protoCommon.OrderStatus_STATUS_CANCELLED
	default:
		return protoCommon.OrderStatus_STATUS_UNSPECIFIED
	}
}

func toProtoOrderType(orderType shared.OrderType) protoCommon.OrderType {
	switch orderType {
	case shared.OrderTypeLimit:
		return protoCommon.OrderType_TYPE_LIMIT
	case shared.OrderTypeMarket:
		return protoCommon.OrderType_TYPE_MARKET
	case shared.OrderTypeStopLoss:
		return protoCommon.OrderType_TYPE_STOP_LOSS
	case shared.OrderTypeTakeProfit:
		return protoCommon.OrderType_TYPE_TAKE_PROFIT
	default:
		return protoCommon.OrderType_TYPE_UNSPECIFIED
	}
}

func toProtoDecimal(value shared.Decimal) *decimal.Decimal {
	return &decimal.Decimal{
		Value: value.String(),
	}
}
