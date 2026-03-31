package kafka

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoEvent "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/events/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func UnmarshalMarketStateChanged(data []byte) (models.MarketStateChangedEvent, error) {
	var protobuf protoEvent.MarketStateChangedEvent
	if err := proto.Unmarshal(data, &protobuf); err != nil {
		return models.MarketStateChangedEvent{}, fmt.Errorf("proto.UnmarshalMarketStateChanged: %w", err)
	}

	return FromProtoMarketStateChanged(&protobuf)
}

func FromProtoMarketStateChanged(msg *protoEvent.MarketStateChangedEvent) (models.MarketStateChangedEvent, error) {
	if msg == nil {
		return models.MarketStateChangedEvent{}, fmt.Errorf("market state changed event is nil")
	}

	eventID, err := parseUUIDRequired("event_id", msg.GetEventId())
	if err != nil {
		return models.MarketStateChangedEvent{}, err
	}

	marketID, err := parseUUIDRequired("market_id", msg.GetMarketId())
	if err != nil {
		return models.MarketStateChangedEvent{}, err
	}

	updatedAt, err := fromProtoTimestamp("updated_at", msg.GetUpdatedAt())
	if err != nil {
		return models.MarketStateChangedEvent{}, err
	}

	deletedAt, err := fromProtoTimestampOptional("deleted_at", msg.GetDeletedAt())
	if err != nil {
		return models.MarketStateChangedEvent{}, err
	}

	return models.MarketStateChangedEvent{
		EventID:   eventID,
		MarketID:  marketID,
		Enabled:   msg.GetEnabled(),
		DeletedAt: deletedAt,
		UpdatedAt: updatedAt,
	}, nil
}

func fromProtoTimestampOptional(fieldName string, timestamp *timestamppb.Timestamp) (*time.Time, error) {
	if timestamp == nil {
		return nil, nil
	}

	if err := timestamp.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	value := timestamp.AsTime().UTC()
	return &value, nil
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

func fromProtoTimestamp(fieldName string, timestamp *timestamppb.Timestamp) (time.Time, error) {
	if timestamp == nil {
		return time.Time{}, fmt.Errorf("%s is required", fieldName)
	}

	if err := timestamp.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	return timestamp.AsTime().UTC(), nil
}
