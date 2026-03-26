package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	orderModel "github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type OrderService struct {
	transactionManager TransactionManager
	saver              Saver
	getter             Getter
	canceler           Canceler
	marketViewer       MarketViewer
	blockStore         MarketBlockStore
	rateLimiters       RateLimiters
	config             Config
	eventProducer      EventProducer
	logger             *zapLogger.Logger
}

type Config struct {
	Timeout     time.Duration
	ServiceName string
}

type RateLimiters struct {
	Create RateLimiter
	Get    RateLimiter
}

type TransactionManager interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Saver interface {
	SaveOrder(ctx context.Context, transaction pgx.Tx, order models.Order) error
}

type MarketBlockStore interface {
	Block(ctx context.Context, marketID uuid.UUID) error
	Unblock(ctx context.Context, marketID uuid.UUID) error
	IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error)
}

type Canceler interface {
	CancelOrderIfActive(ctx context.Context, transaction pgx.Tx, orderID uuid.UUID) (bool, error)
}

type Getter interface {
	GetOrder(ctx context.Context, id, userID uuid.UUID) (models.Order, error)
}

type MarketViewer interface {
	GetMarketByID(ctx context.Context, id uuid.UUID) (sharedModels.Market, error)
}

type RateLimiter interface {
	Allow(ctx context.Context, userID uuid.UUID) (bool, error)
	Limit() int64
	Window() time.Duration
}

type EventProducer interface {
	ProduceOrderCreated(ctx context.Context, transaction pgx.Tx, event models.OrderCreatedEvent) error
	ProduceOrderStatusUpdated(ctx context.Context, transaction pgx.Tx, event models.OrderStatusUpdatedEvent) error
}

func New(
	manager TransactionManager,
	saver Saver,
	getter Getter,
	canceler Canceler,
	viewer MarketViewer,
	store MarketBlockStore,
	limiters RateLimiters,
	cfg Config,
	producer EventProducer,
	logger *zapLogger.Logger,
) *OrderService {
	return &OrderService{
		transactionManager: manager,
		saver:              saver,
		getter:             getter,
		canceler:           canceler,
		marketViewer:       viewer,
		blockStore:         store,
		rateLimiters:       limiters,
		config:             cfg,
		eventProducer:      producer,
		logger:             logger,
	}
}

func (s *OrderService) CreateOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
) (uuid.UUID, orderModel.OrderStatus, error) {
	const op = "OrderService.CreateOrder"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	if err := s.checkRateLimit(ctx, userID, s.rateLimiters.Create, "create_order"); err != nil {
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err := s.validateMarket(ctx, marketID); err != nil {
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err := s.ensureMarketNotBlocked(ctx, marketID); err != nil {
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	orderID, correlationID, orderStatus, err := s.saveOrder(ctx, userID, marketID, orderType, price, quantity)
	if err != nil {
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	orderStatus = s.checkOrderAfterCommit(ctx, marketID, orderID, correlationID, orderStatus)

	metrics.OrdersCreatedTotal.WithLabelValues(s.config.ServiceName, marketID.String()).Inc()

	return orderID, orderStatus, nil
}

func (s *OrderService) GetOrderStatus(
	ctx context.Context,
	orderID, userID uuid.UUID,
) (orderModel.OrderStatus, error) {
	const op = "OrderService.GetOrderStatus"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	if err := s.checkRateLimit(ctx, userID, s.rateLimiters.Get, "get_order_status"); err != nil {
		return orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	order, err := s.fetchOrder(ctx, orderID, userID)
	if err != nil {
		return orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	return order.Status, nil
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

	order, err := s.getter.GetOrder(ctx, orderID, userID)
	if err != nil {
		tracing.RecordError(span, err)
		if errors.Is(err, repositoryErrors.ErrOrderNotFound) {
			return models.Order{}, sharedErrors.ErrNotFound{ID: orderID}
		}

		return models.Order{}, err
	}

	span.SetAttributes(
		attribute.String("order_status", order.Status.String()),
		attribute.String("order_type", order.Type.String()),
	)

	return order, nil
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
		tracing.RecordError(span, err)
		return err
	}
	if !allowed {
		err = serviceErrors.ErrLimitExceeded{
			Limit:  limit,
			Window: window,
		}
		tracing.RecordError(span, err)
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
			attributeUUID("market_id", marketID)),
	)
	defer span.End()

	market, err := s.marketViewer.GetMarketByID(ctx, marketID)
	if err != nil {
		tracing.RecordError(span, err)
		return err
	}

	span.SetAttributes(
		attribute.Bool("market_enabled", market.Enabled),
		attribute.Bool("market_deleted", market.DeletedAt != nil),
	)
	if !market.Enabled || market.DeletedAt != nil {
		err = sharedErrors.ErrMarketNotFound{ID: marketID}
		tracing.RecordError(span, err)
		return err
	}

	return nil
}

func (s *OrderService) ensureMarketNotBlocked(
	ctx context.Context,
	marketID uuid.UUID,
) error {
	ctx, span := tracing.StartSpan(ctx, "order.ensure_market_not_blocked",
		trace.WithAttributes(
			attributeUUID("market_id", marketID),
		),
	)
	defer span.End()

	blocked, err := s.blockStore.IsBlocked(ctx, marketID)
	if err != nil {
		tracing.RecordError(span, err)
		return err
	}

	span.SetAttributes(attribute.Bool("market_blocked", blocked))

	if blocked {
		err = sharedErrors.ErrMarketNotFound{ID: marketID}
		tracing.RecordError(span, err)

		return err
	}

	return nil
}

// saveOrder сохраняет заказ и пишет OrderCreatedEvent в outbox в одной транзакции
func (s *OrderService) saveOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
) (uuid.UUID, uuid.UUID, orderModel.OrderStatus, error) {
	const op = "OrderService.saveOrder"

	ctx, span := tracing.StartSpan(ctx, "order.save_order_transaction")
	defer span.End()

	now := time.Now().UTC()
	correlationID := uuid.New()

	order := buildOrder(userID, marketID, orderType, price, quantity, now)
	event := buildOrderCreatedEvent(order, correlationID, now)

	transaction, err := s.transactionManager.Begin(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: begin transaction: %w", op, err)
	}

	defer RollbackTx(ctx, transaction, s.logger, "saveOrder: transaction rollback failed", s.config.Timeout)

	if err = s.saver.SaveOrder(ctx, transaction, order); err != nil {
		tracing.RecordError(span, err)
		if errors.Is(err, repositoryErrors.ErrOrderAlreadyExists) {
			return uuid.Nil, uuid.Nil, orderModel.OrderStatusUnspecified, sharedErrors.ErrAlreadyExists{ID: order.ID}
		}

		return uuid.Nil, uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err = s.eventProducer.ProduceOrderCreated(ctx, transaction, event); err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err = transaction.Commit(ctx); err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: commit transaction: %w", op, err)
	}

	return order.ID, correlationID, order.Status, nil
}

func RollbackTx(ctx context.Context,
	transaction pgx.Tx,
	logger *zapLogger.Logger,
	message string,
	timeout time.Duration,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	if err := transaction.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		logger.Error(cleanupCtx, message, zap.Error(err))
	}
}

func (s *OrderService) checkOrderAfterCommit(
	ctx context.Context,
	marketID, orderID, correlationID uuid.UUID,
	currentStatus orderModel.OrderStatus,
) orderModel.OrderStatus {
	recheckCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.Timeout)
	defer cancel()

	blocked, err := s.blockStore.IsBlocked(recheckCtx, marketID)
	if err != nil {
		s.logger.Error(recheckCtx, "Post-commit market block recheck failed",
			zap.String("market_id", marketID.String()),
			zap.String("order_id", orderID.String()),
			zap.Error(err),
		)
		return currentStatus
	}

	if !blocked {
		return currentStatus
	}

	cancelled, cancelErr := s.cancelOrderAfterCommit(recheckCtx, orderID, correlationID)
	if cancelErr != nil {
		s.logger.Error(recheckCtx, "Failed to cancel newly created order after market block",
			zap.String("market_id", marketID.String()),
			zap.String("order_id", orderID.String()),
			zap.Error(cancelErr),
		)
		return currentStatus
	}

	if cancelled {
		s.logger.Warn(recheckCtx, "Order was cancelled after commit because market is blocked",
			zap.String("market_id", marketID.String()),
			zap.String("order_id", orderID.String()),
		)
		return orderModel.OrderStatusCancelled
	}

	return currentStatus
}

func (s *OrderService) cancelOrderAfterCommit(
	ctx context.Context,
	orderID uuid.UUID,
	correlationID uuid.UUID,
) (bool, error) {
	const op = "OrderService.cancelOrderAfterCommit"

	ctx, span := tracing.StartSpan(ctx, "order.cancel_after_commit",
		trace.WithAttributes(
			attributeUUID("order_id", orderID),
		),
	)
	defer span.End()

	transaction, err := s.transactionManager.Begin(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return false, fmt.Errorf("%s: begin transaction: %w", op, err)
	}
	defer RollbackTx(ctx, transaction, s.logger, "cancelOrderAfterCommit: transaction rollback failed", s.config.Timeout)

	cancelled, err := s.canceler.CancelOrderIfActive(ctx, transaction, orderID)
	if err != nil {
		tracing.RecordError(span, err)
		return false, fmt.Errorf("%s: %w", op, err)
	}
	if cancelled {
		statusEvent := buildOrderStatusUpdatedEvent(
			orderID,
			orderModel.OrderStatusCancelled,
			"market blocked during post-commit recheck",
			correlationID,
			nil,
			time.Now().UTC(),
		)

		if err = s.eventProducer.ProduceOrderStatusUpdated(ctx, transaction, statusEvent); err != nil {
			tracing.RecordError(span, err)
			return false, fmt.Errorf("%s: produce OrderStatusUpdatedEvent: %w", op, err)
		}
	}

	if err = transaction.Commit(ctx); err != nil {
		tracing.RecordError(span, err)
		return false, fmt.Errorf("%s: commit transaction: %w", op, err)
	}

	span.SetAttributes(attribute.Bool("order_cancelled", cancelled))

	return cancelled, nil
}

func buildOrder(
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
	now time.Time,
) models.Order {
	return models.Order{
		ID:        uuid.New(),
		UserID:    userID,
		MarketID:  marketID,
		Type:      orderType,
		Price:     price,
		Quantity:  quantity,
		Status:    orderModel.OrderStatusCreated,
		CreatedAt: now,
	}
}

func buildOrderCreatedEvent(
	order models.Order,
	correlationID uuid.UUID,
	now time.Time,
) models.OrderCreatedEvent {
	return models.OrderCreatedEvent{
		EventID:       uuid.New(),
		OrderID:       order.ID,
		UserID:        order.UserID,
		MarketID:      order.MarketID,
		Type:          order.Type,
		Price:         order.Price,
		Quantity:      order.Quantity,
		Status:        order.Status,
		CorrelationID: correlationID,
		CausationID:   nil,
		CreatedAt:     now,
	}
}

func buildOrderStatusUpdatedEvent(
	orderID uuid.UUID,
	newStatus orderModel.OrderStatus,
	reason string,
	correlationID uuid.UUID,
	causationID *uuid.UUID,
	now time.Time,
) models.OrderStatusUpdatedEvent {
	return models.OrderStatusUpdatedEvent{
		EventID:       uuid.New(),
		OrderID:       orderID,
		NewStatus:     newStatus,
		Reason:        reason,
		CorrelationID: correlationID,
		CausationID:   causationID,
		UpdatedAt:     now,
	}
}

func attributeUUID(key string, id uuid.UUID) attribute.KeyValue {
	return attribute.String(key, id.String())
}
