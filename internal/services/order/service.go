package order

import (
	"context"
	"errors"
	"fmt"
	"spotOrder/internal/domain/models"
	"spotOrder/internal/storage"
	"spotOrder/internal/storage/db"
	"time"

	"github.com/google/uuid"
	spot_orderv1 "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/genproto/googleapis/type/decimal"
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
	GetOrder(ctx context.Context, id string) (models.Order, error)
}

type MarketViewer interface {
	ViewMarkets(ctx context.Context, roles []spot_orderv1.UserRole) (*spot_orderv1.ViewMarketsResponse, error)
}

var (
	ErrMarketNotFound     = errors.New("market not found")
	ErrOrderNotFound      = errors.New("order not found")
	ErrOrderAlreadyExists = errors.New("order already exists")
)

func NewService(orderStore *db.OrderStore, marketViewer MarketViewer) *Service {
	return &Service{
		saver:        orderStore,
		getter:       orderStore,
		marketViewer: marketViewer,
	}
}

func (s *Service) CreateOrder(
	ctx context.Context,
	userID, marketID string,
	orderType spot_orderv1.OrderType,
	price *decimal.Decimal,
	quantity int64,
) (string, spot_orderv1.OrderStatus, error) {
	const op = "Service.CreateOrder"

	resp, err := s.marketViewer.ViewMarkets(ctx, nil) // роли пока не используем
	if err != nil {
		return "", spot_orderv1.OrderStatus_STATUS_CANCELLED, fmt.Errorf("%s: %w", op, err)
	}

	found := false
	for _, market := range resp.GetMarkets() {
		if market.GetId() == marketID {
			found = true
			break
		}
	}
	if !found {
		return "", spot_orderv1.OrderStatus_STATUS_CANCELLED, fmt.Errorf("%s: %w", op, ErrMarketNotFound)
	}

	orderID := uuid.NewString()
	status := spot_orderv1.OrderStatus_STATUS_CREATED

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
		if errors.Is(err, storage.ErrOrderAlreadyExists) {
			return "", spot_orderv1.OrderStatus_STATUS_CANCELLED, fmt.Errorf("%s: %w", op, ErrOrderAlreadyExists)
		}

		return "", spot_orderv1.OrderStatus_STATUS_CANCELLED, fmt.Errorf("%s: %w", op, err)
	}

	return orderID, status, nil
}

func (s *Service) GetOrderStatus(ctx context.Context, orderID, userID string) (spot_orderv1.OrderStatus, error) {
	const op = "Service.GetOrderStatus"

	order, err := s.getter.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, storage.ErrOrderNotFound) {
			return spot_orderv1.OrderStatus_STATUS_UNSPECIFIED, fmt.Errorf("%s: %w", op, ErrOrderNotFound)
		}

		return spot_orderv1.OrderStatus_STATUS_UNSPECIFIED, fmt.Errorf("%s: %w", op, err)
	}

	if order.UserID != userID {
		return spot_orderv1.OrderStatus_STATUS_UNSPECIFIED, fmt.Errorf("%s: %w", op, ErrOrderNotFound)
	}

	return order.Status, nil
}
