package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type Service struct {
	saver        Saver
	getter       Getter
	marketViewer MarketViewer

	createRateLimiter RateLimiter
	getRateLimiter    RateLimiter
	createTimeout     time.Duration
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

type RateLimiter interface {
	Allow(ctx context.Context, userID uuid.UUID) (bool, error)
}

func NewService(s Saver, g Getter, mv MarketViewer, create, get RateLimiter, t time.Duration) *Service {
	return &Service{
		saver:             s,
		getter:            g,
		marketViewer:      mv,
		createRateLimiter: create,
		getRateLimiter:    get,
		createTimeout:     t,
	}
}

func (s *Service) CreateOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType models.OrderType,
	price models.Decimal,
	quantity int64,
) (uuid.UUID, models.OrderStatus, error) {
	const op = "Service.CreateOrder"

	ctx, cancel := context.WithTimeout(ctx, s.createTimeout)
	defer cancel()

	if err := s.checkCreateRateLimit(ctx, userID); err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	if err := s.validateMarket(ctx, marketID); err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	orderID, orderStatus, err := s.saveOrder(ctx, userID, marketID, orderType, price, quantity)
	if err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	return orderID, orderStatus, nil
}

func (s *Service) GetOrderStatus(ctx context.Context, orderID, userID uuid.UUID) (models.OrderStatus, error) {
	const op = "Service.GetOrderStatus"

	if err := s.checkGetRateLimit(ctx, userID); err != nil {
		return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	order, err := s.fetchOrder(ctx, orderID, userID)
	if err != nil {
		return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	return order.Status, nil
}

func (s *Service) checkCreateRateLimit(ctx context.Context, userID uuid.UUID) error {
	allowed, err := s.createRateLimiter.Allow(ctx, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return serviceErrors.ErrRateLimitExceeded
	}

	return nil
}

func (s *Service) checkGetRateLimit(ctx context.Context, userID uuid.UUID) error {
	allowed, err := s.getRateLimiter.Allow(ctx, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return serviceErrors.ErrRateLimitExceeded
	}

	return nil
}

func (s *Service) validateMarket(ctx context.Context, marketID uuid.UUID) error {
	// Заглушка
	markets, err := s.marketViewer.ViewMarkets(ctx, []sharedModels.UserRole{sharedModels.UserRoleUser})
	if err != nil {
		return err
	}

	for _, market := range markets {
		if market.ID == marketID {
			return nil
		}
	}

	return serviceErrors.ErrMarketsNotFound
}

func (s *Service) saveOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType models.OrderType,
	price models.Decimal,
	quantity int64,
) (uuid.UUID, models.OrderStatus, error) {
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

	if err := s.saver.SaveOrder(ctx, newOrder); err != nil {
		if errors.Is(err, repositoryErrors.ErrOrderAlreadyExists) {
			return uuid.Nil, models.OrderStatusCancelled, serviceErrors.ErrOrderAlreadyExists
		}

		return uuid.Nil, models.OrderStatusCancelled, err
	}

	return orderID, orderStatus, nil
}

func (s *Service) fetchOrder(ctx context.Context, orderID, userID uuid.UUID) (models.Order, error) {
	order, err := s.getter.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrOrderNotFound) {
			return models.Order{}, serviceErrors.ErrOrderNotFound
		}

		return models.Order{}, err
	}

	if order.UserID != userID {
		return models.Order{}, serviceErrors.ErrOrderNotFound
	}

	return order, nil
}
