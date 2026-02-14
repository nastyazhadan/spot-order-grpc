package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	storageErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/storage"

	"github.com/google/uuid"
)

type Service struct {
	saver        Saver
	getter       Getter
	marketViewer MarketViewer
}

type Saver interface {
	SaveOrder(ctx context.Context, order models.Order) error
}

type Getter interface {
	GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error)
}

type MarketViewer interface {
	ViewMarkets(ctx context.Context, roles []int32) ([]models.Market, error)
}

func NewService(s Saver, g Getter, mv MarketViewer) *Service {
	return &Service{
		saver:        s,
		getter:       g,
		marketViewer: mv,
	}
}

func (s *Service) CreateOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType models.Type,
	price models.Decimal,
	quantity int64,
) (uuid.UUID, models.Status, error) {
	const op = "Service.CreateOrder"

	markets, err := s.marketViewer.ViewMarkets(ctx, []int32{})
	if err != nil {
		return uuid.Nil, models.StatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	found := false
	for _, m := range markets {
		if m.ID == marketID {
			found = true
			break
		}
	}
	if !found {
		return uuid.Nil, models.StatusCancelled, fmt.Errorf("%s: %w", op, serviceErrors.ErrMarketsNotFound)
	}

	orderID := uuid.New()
	status := models.StatusCreated

	newOrder := models.Order{
		ID:        orderID,
		UserID:    userID,
		MarketID:  marketID,
		Type:      orderType,
		Price:     price,
		Quantity:  quantity,
		Status:    status,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.saver.SaveOrder(ctx, newOrder); err != nil {
		if errors.Is(err, storageErrors.ErrOrderAlreadyExists) {
			return uuid.Nil, models.StatusCancelled, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderAlreadyExists)
		}

		return uuid.Nil, models.StatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	return orderID, status, nil
}

func (s *Service) GetOrderStatus(ctx context.Context, orderID, userID uuid.UUID) (models.Status, error) {
	const op = "Service.GetOrderStatus"

	result, err := s.getter.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, storageErrors.ErrOrderNotFound) {
			return models.StatusUnspecified, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderNotFound)
		}

		return models.StatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if result.UserID != userID {
		return models.StatusUnspecified, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderNotFound)
	}

	return result.Status, nil
}
