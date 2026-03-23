package kafka

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	protoEvent "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/events/v1"
)

func Unmarshal(data []byte) (models.OrderStatusUpdatedEvent, error) {
	var protobuf protoEvent.OrderStatusUpdatedEvent
	if err := proto.Unmarshal(data, &protobuf); err != nil {
		return models.OrderStatusUpdatedEvent{}, fmt.Errorf("proto.Unmarshal OrderStatusUpdatedEvent: %w", err)
	}

	return FromProtoOrderStatusUpdated(&protobuf)
}

func FromProtoOrderCreatedEvent(msg *protoEvent.OrderCreatedEvent) (models.OrderCreatedEvent, error) {
	if msg == nil {
		return models.OrderCreatedEvent{}, fmt.Errorf("order created event is nil")
	}

	eventID, err := parseUUIDRequired("event_id", msg.GetEventId())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	orderID, err := parseUUIDRequired("order_id", msg.GetOrderId())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	userID, err := parseUUIDRequired("user_id", msg.GetUserId())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	marketID, err := parseUUIDRequired("market_id", msg.GetMarketId())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	correlationID, err := parseUUIDRequired("correlation_id", msg.GetCorrelationId())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	causationID, err := parseUUIDOptional(msg.GetCausationId())
	if err != nil {
		return models.OrderCreatedEvent{}, fmt.Errorf("invalid causation_id: %w", err)
	}

	price, err := fromProtoDecimal(msg.GetPrice())
	if err != nil {
		return models.OrderCreatedEvent{}, fmt.Errorf("invalid price: %w", err)
	}

	orderType, err := fromProtoOrderType(msg.GetOrderType())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	status, err := fromProtoOrderStatus(msg.GetStatus())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	createdAt, err := fromProtoTimestamp("created_at", msg.GetCreatedAt())
	if err != nil {
		return models.OrderCreatedEvent{}, err
	}

	return models.OrderCreatedEvent{
		EventID:       eventID,
		OrderID:       orderID,
		UserID:        userID,
		MarketID:      marketID,
		Type:          orderType,
		Price:         price,
		Quantity:      msg.GetQuantity(),
		Status:        status,
		CorrelationID: correlationID,
		CausationID:   causationID,
		CreatedAt:     createdAt,
	}, nil
}

func FromProtoOrderStatusUpdated(
	msg *protoEvent.OrderStatusUpdatedEvent,
) (models.OrderStatusUpdatedEvent, error) {
	if msg == nil {
		return models.OrderStatusUpdatedEvent{}, fmt.Errorf("order status updated event is nil")
	}

	eventID, err := parseUUIDRequired("event_id", msg.GetEventId())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, err
	}

	orderID, err := parseUUIDRequired("order_id", msg.GetOrderId())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, err
	}

	sagaID, err := parseUUIDRequired("saga_id", msg.GetSagaId())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, err
	}

	correlationID, err := parseUUIDRequired("correlation_id", msg.GetCorrelationId())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, err
	}

	causationID, err := parseUUIDOptional(msg.GetCausationId())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, fmt.Errorf("invalid causation_id: %w", err)
	}

	newStatus, err := fromProtoOrderStatus(msg.GetNewStatus())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, err
	}

	updatedAt, err := fromProtoTimestamp("updated_at", msg.GetUpdatedAt())
	if err != nil {
		return models.OrderStatusUpdatedEvent{}, err
	}

	return models.OrderStatusUpdatedEvent{
		EventID:       eventID,
		OrderID:       orderID,
		SagaID:        sagaID,
		NewStatus:     newStatus,
		Reason:        msg.GetReason(),
		CorrelationID: correlationID,
		CausationID:   causationID,
		UpdatedAt:     updatedAt,
	}, nil
}

func parseUUIDRequired(fieldName, raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, fmt.Errorf("%s is required", fieldName)
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	return id, nil
}

func parseUUIDOptional(raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

func fromProtoDecimal(value *decimal.Decimal) (shared.Decimal, error) {
	if value == nil {
		return shared.Decimal{}, fmt.Errorf("price is nil")
	}

	if value.Value == "" {
		return shared.Decimal{}, fmt.Errorf("price value is empty")
	}

	parsed, err := shared.NewDecimal(value.Value)
	if err != nil {
		return shared.Decimal{}, err
	}

	return parsed, nil
}

func fromProtoTimestamp(fieldName string, timestamp *timestamppb.Timestamp) (time.Time, error) {
	if timestamp == nil {
		return time.Time{}, fmt.Errorf("%s is required", fieldName)
	}

	if err := timestamp.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	return timestamp.AsTime().UTC(), nil
}

func fromProtoOrderStatus(status protoEvent.OrderStatus) (shared.OrderStatus, error) {
	switch status {
	case protoEvent.OrderStatus_STATUS_CREATED:
		return shared.OrderStatusCreated, nil
	case protoEvent.OrderStatus_STATUS_PENDING:
		return shared.OrderStatusPending, nil
	case protoEvent.OrderStatus_STATUS_FILLED:
		return shared.OrderStatusFilled, nil
	case protoEvent.OrderStatus_STATUS_CANCELLED:
		return shared.OrderStatusCancelled, nil
	default:
		return shared.OrderStatusUnspecified, fmt.Errorf("unknown order status: %s", status.String())
	}
}

func fromProtoOrderType(orderType protoEvent.OrderType) (shared.OrderType, error) {
	switch orderType {
	case protoEvent.OrderType_TYPE_LIMIT:
		return shared.OrderTypeLimit, nil
	case protoEvent.OrderType_TYPE_MARKET:
		return shared.OrderTypeMarket, nil
	case protoEvent.OrderType_TYPE_STOP_LOSS:
		return shared.OrderTypeStopLoss, nil
	case protoEvent.OrderType_TYPE_TAKE_PROFIT:
		return shared.OrderTypeTakeProfit, nil
	default:
		return shared.OrderTypeUnspecified, fmt.Errorf("unknown order type: %s", orderType.String())
	}
}
