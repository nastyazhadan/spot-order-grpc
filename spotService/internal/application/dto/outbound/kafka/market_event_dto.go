package kafka

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoEvent "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/events/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func Marshal(event models.MarketStateChangedEvent) ([]byte, error) {
	payload, err := proto.Marshal(ToProto(event))
	if err != nil {
		return nil, fmt.Errorf("marshal MarketStateChangedEvent: %w", err)
	}

	return payload, nil
}

func ToProto(event models.MarketStateChangedEvent) *protoEvent.MarketStateChangedEvent {
	var deletedAt *timestamppb.Timestamp
	if event.DeletedAt != nil {
		deletedAt = timestamppb.New(event.DeletedAt.UTC())
	}

	return &protoEvent.MarketStateChangedEvent{
		EventId:   event.EventID.String(),
		MarketId:  event.MarketID.String(),
		Enabled:   event.Enabled,
		DeletedAt: deletedAt,
		UpdatedAt: timestamppb.New(event.UpdatedAt.UTC()),
	}
}
