package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	storageErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
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
	ViewMarkets(ctx context.Context, roles []sharedModels.UserRole) ([]sharedModels.Market, error)
}

func NewService(s Saver, g Getter, mv MarketViewer) *Service {
	return &Service{
		saver:        s,
		getter:       g,
		marketViewer: mv,
	}
}

func (service *Service) CreateOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType models.OrderType,
	price models.Decimal,
	quantity int64,
) (uuid.UUID, models.OrderStatus, error) {
	const op = "Service.CreateOrder"

	currentUserRole := sharedModels.UserRoleUser // Заглушка
	if currentUserRole != sharedModels.UserRoleUser {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, serviceErrors.ErrCreatingOrderNotRequired)
	}

	markets, err := service.marketViewer.ViewMarkets(ctx, []sharedModels.UserRole{currentUserRole})
	if err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	found := false
	for _, market := range markets {
		if market.ID == marketID {
			found = true
			break
		}
	}
	if !found {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, serviceErrors.ErrMarketsNotFound)
	}

	orderID := uuid.New()
	orderStatus := models.OrderStatusCreated

	newOrder := models.Order{
		ID:        orderID,
		UserID:    userID,
		MarketID:  marketID,
		Type:      orderType,
		Price:     price,
		Quantity:  quantity,
		Status:    orderStatus,
		CreatedAt: time.Now().UTC(),
	}
	if err := service.saver.SaveOrder(ctx, newOrder); err != nil {
		if errors.Is(err, storageErrors.ErrOrderAlreadyExists) {
			return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderAlreadyExists)
		}

		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	return orderID, orderStatus, nil
}

func (service *Service) GetOrderStatus(ctx context.Context, orderID, userID uuid.UUID) (models.OrderStatus, error) {
	const op = "Service.GetOrderStatus"

	result, err := service.getter.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, storageErrors.ErrOrderNotFound) {
			return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderNotFound)
		}

		return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if result.UserID != userID {
		return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderNotFound)
	}

	return result.Status, nil
}
