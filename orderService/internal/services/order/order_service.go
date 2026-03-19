package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type OrderService struct {
	saver        Saver
	getter       Getter
	marketViewer MarketViewer
	rateLimiters RateLimiters
	config       OrderServiceConfig
	logger       *zapLogger.Logger
}

type OrderServiceConfig struct {
	Timeout     time.Duration
	ServiceName string
}

type RateLimiters struct {
	Create RateLimiter
	Get    RateLimiter
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
	Limit() int64
	Window() time.Duration
}

func NewOrderService(
	saver Saver,
	getter Getter,
	viewer MarketViewer,
	limiters RateLimiters,
	config OrderServiceConfig,
	logger *zapLogger.Logger,
) *OrderService {
	return &OrderService{
		saver:        saver,
		getter:       getter,
		marketViewer: viewer,
		rateLimiters: limiters,
		config:       config,
		logger:       logger,
	}
}

func (s *OrderService) CreateOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType models.OrderType,
	price models.Decimal,
	quantity int64,
) (uuid.UUID, models.OrderStatus, error) {
	const op = "OrderService.CreateOrder"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	if err := s.checkRateLimit(ctx, userID, s.rateLimiters.Create, "create_order"); err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	if err := s.validateMarket(ctx, marketID); err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	orderID, orderStatus, err := s.saveOrder(ctx, userID, marketID, orderType, price, quantity)
	if err != nil {
		return uuid.Nil, models.OrderStatusCancelled, fmt.Errorf("%s: %w", op, err)
	}

	metrics.OrdersCreatedTotal.WithLabelValues(s.config.ServiceName).Inc()

	return orderID, orderStatus, nil
}

func (s *OrderService) GetOrderStatus(
	ctx context.Context,
	orderID, userID uuid.UUID,
) (models.OrderStatus, error) {
	const op = "OrderService.GetOrderStatus"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	if err := s.checkRateLimit(ctx, userID, s.rateLimiters.Get, "get_order_status"); err != nil {
		return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	order, err := s.fetchOrder(ctx, orderID, userID)
	if err != nil {
		return models.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	return order.Status, nil
}

func (s *OrderService) checkRateLimit(
	ctx context.Context,
	userID uuid.UUID,
	limiter RateLimiter,
	method string,
) error {
	limit := limiter.Limit()
	window := limiter.Window()

	ctx, span := tracing.StartSpan(ctx, "order.check_rate_limit",
		trace.WithAttributes(
			attributeUUID("user_id", userID),
			attribute.Int64("limit", limit),
			attribute.String("window", window.String()),
		),
	)
	defer span.End()

	allowed, err := limiter.Allow(ctx, userID)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if !allowed {
		err = serviceErrors.ErrLimitExceeded{
			Limit:  limit,
			Window: window,
		}
		span.RecordError(err)
		metrics.RateLimitRejectedTotal.WithLabelValues(s.config.ServiceName, method).Inc()
		return err
	}

	return nil
}

func (s *OrderService) validateMarket(
	ctx context.Context,
	marketID uuid.UUID,
) error {
	ctx, span := tracing.StartSpan(ctx, "order.validate_market",
		trace.WithAttributes(
			attributeUUID("market_id", marketID),
		),
	)
	defer span.End()

	markets, err := s.marketViewer.ViewMarkets(ctx, []sharedModels.UserRole{sharedModels.UserRoleUser})
	if err != nil {
		span.RecordError(err)
		return err
	}

	for _, market := range markets {
		if market.ID == marketID {
			return nil
		}
	}

	err = serviceErrors.ErrMarketNotFound{ID: marketID}
	span.RecordError(err)
	return err
}

func (s *OrderService) saveOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType models.OrderType,
	price models.Decimal,
	quantity int64,
) (uuid.UUID, models.OrderStatus, error) {
	orderID := uuid.New()
	orderStatus := models.OrderStatusCreated

	ctx, span := tracing.StartSpan(ctx, "order.save_order",
		trace.WithAttributes(
			attributeUUID("order_id", orderID),
			attributeUUID("user_id", userID),
			attributeUUID("market_id", marketID),
			attribute.String("price", price.String()),
			attribute.Int64("quantity", quantity),
		),
	)
	defer span.End()

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
		span.RecordError(err)
		if errors.Is(err, repositoryErrors.ErrOrderAlreadyExists) {
			return uuid.Nil, models.OrderStatusCancelled, serviceErrors.ErrAlreadyExists{ID: orderID}
		}

		return uuid.Nil, models.OrderStatusCancelled, err
	}

	return orderID, orderStatus, nil
}

func (s *OrderService) fetchOrder(
	ctx context.Context,
	orderID, userID uuid.UUID,
) (models.Order, error) {
	ctx, span := tracing.StartSpan(ctx, "order.fetch_order",
		trace.WithAttributes(
			attributeUUID("user_id", userID),
			attributeUUID("order_id", orderID),
		),
	)
	defer span.End()

	order, err := s.getter.GetOrder(ctx, orderID)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, repositoryErrors.ErrOrderNotFound) {
			return models.Order{}, serviceErrors.ErrNotFound{ID: orderID}
		}

		return models.Order{}, err
	}

	if order.UserID != userID {
		err = serviceErrors.ErrNotFound{ID: orderID}
		span.RecordError(err)
		return models.Order{}, err
	}

	return order, nil
}

func attributeUUID(key string, id uuid.UUID) attribute.KeyValue {
	return attribute.String(key, id.String())
}
