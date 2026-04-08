package mapper

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func MarketFromProto(market *proto.Market) (models.Market, error) {
	if market == nil {
		return models.Market{}, errors.New("proto market is nil")
	}

	id, err := uuid.Parse(market.GetId())
	if err != nil {
		return models.Market{}, fmt.Errorf("invalid market id %q: %w", market.GetId(), err)
	}

	updatedAt, err := requiredTimestampUTC("updated_at", market.GetUpdatedAt())
	if err != nil {
		return models.Market{}, err
	}

	deletedAt, err := optionalTimestampUTC("deleted_at", market.GetDeletedAt())
	if err != nil {
		return models.Market{}, err
	}

	return models.Market{
		ID:        id,
		Name:      market.GetName(),
		Enabled:   market.GetEnabled(),
		DeletedAt: deletedAt,
		UpdatedAt: updatedAt,
	}, nil
}

func requiredTimestampUTC(field string, timestamp *timestamppb.Timestamp) (time.Time, error) {
	if timestamp == nil {
		return time.Time{}, fmt.Errorf("%s is required", field)
	}

	if err := timestamp.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", field, err)
	}

	return timestamp.AsTime().UTC(), nil
}

func optionalTimestampUTC(field string, timestamp *timestamppb.Timestamp) (*time.Time, error) {
	if timestamp == nil {
		return nil, nil
	}

	if err := timestamp.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", field, err)
	}

	t := timestamp.AsTime().UTC()
	return &t, nil
}
