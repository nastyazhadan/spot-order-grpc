package kafka

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
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
		EventId:       event.EventID.String(),
		OrderId:       event.OrderID.String(),
		UserId:        event.UserID.String(),
		MarketId:      event.MarketID.String(),
		OrderType:     toProtoOrderType(event.Type),
		Price:         toProtoDecimal(event.Price),
		Quantity:      event.Quantity,
		Status:        toProtoOrderStatus(event.Status),
		CorrelationId: event.CorrelationID.String(),
		CausationId:   uuidPtrToString(event.CausationID),
		CreatedAt:     timestamppb.New(event.CreatedAt.UTC()),
	}
}

func ToProtoOrderStatusUpdated(event models.OrderStatusUpdatedEvent) *protoEvent.OrderStatusUpdatedEvent {
	return &protoEvent.OrderStatusUpdatedEvent{
		EventId:       event.EventID.String(),
		OrderId:       event.OrderID.String(),
		NewStatus:     toProtoOrderStatus(event.NewStatus),
		Reason:        event.Reason,
		CorrelationId: event.CorrelationID.String(),
		CausationId:   uuidPtrToString(event.CausationID),
		UpdatedAt:     timestamppb.New(event.UpdatedAt),
	}
}

// OrderCreatedKey - Использование order_id как ключа гарантирует, что все сообщения
// для одной заявки попадают в одну партицию (ordering per order)
func OrderCreatedKey(orderID uuid.UUID) []byte {
	return []byte(orderID.String())
}

func toProtoOrderStatus(status shared.OrderStatus) protoEvent.OrderStatus {
	switch status {
	case shared.OrderStatusCreated:
		return protoEvent.OrderStatus_STATUS_CREATED
	case shared.OrderStatusPending:
		return protoEvent.OrderStatus_STATUS_PENDING
	case shared.OrderStatusFilled:
		return protoEvent.OrderStatus_STATUS_FILLED
	case shared.OrderStatusCancelled:
		return protoEvent.OrderStatus_STATUS_CANCELLED
	default:
		return protoEvent.OrderStatus_STATUS_UNSPECIFIED
	}
}

func toProtoOrderType(orderType shared.OrderType) protoEvent.OrderType {
	switch orderType {
	case shared.OrderTypeLimit:
		return protoEvent.OrderType_TYPE_LIMIT
	case shared.OrderTypeMarket:
		return protoEvent.OrderType_TYPE_MARKET
	case shared.OrderTypeStopLoss:
		return protoEvent.OrderType_TYPE_STOP_LOSS
	case shared.OrderTypeTakeProfit:
		return protoEvent.OrderType_TYPE_TAKE_PROFIT
	default:
		return protoEvent.OrderType_TYPE_UNSPECIFIED
	}
}

func toProtoDecimal(value shared.Decimal) *decimal.Decimal {
	return &decimal.Decimal{
		Value: value.String(),
	}
}

func uuidPtrToString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
