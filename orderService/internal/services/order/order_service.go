package order

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type OrderService struct {
	transactionManager TransactionManager
	saver              Saver
	getter             Getter
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
	SyncState(ctx context.Context, marketID uuid.UUID, blocked bool, updatedAt time.Time) (bool, error)
	IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error)
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

	orderID, orderStatus, err := s.saveOrder(ctx, userID, marketID, orderType, price, quantity)
	if err != nil {
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

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
			attributes.UserIDValue(userID.String()),
			attributes.OrderIDValue(orderID.String()),
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
		attributes.OrderStatusValue(order.Status.String()),
		attributes.OrderTypeValue(order.Type.String()),
	)

	return order, nil
}

func (s *OrderService) checkRateLimit(
	ctx context.Context,
	userID uuid.UUID,
	limiter RateLimiter,
	operation string,
) error {
	limit := limiter.Limit()
	window := limiter.Window()

	ctx, span := tracing.StartSpan(ctx, "order.check_rate_limit",
		trace.WithAttributes(
			attributes.UserIDValue(userID.String()),
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
		metrics.RateLimitRejectedBusinessTotal.WithLabelValues(s.config.ServiceName, operation).Inc()
		return err
	}

	return nil
}

func (s *OrderService) validateMarket(
	ctx context.Context,
	marketID uuid.UUID,
) error {
	ctx, span := tracing.StartSpan(ctx, "order.validate_market",
		trace.WithAttributes(attributes.MarketIDValue(marketID.String())),
	)
	defer span.End()

	blocked, err := s.getMarketBlockedState(ctx, span, marketID)
	if err != nil {
		return err
	}

	// Еще раз проверяем доступность рынка, т.к. redis может быть неактуальным
	market, err := s.marketViewer.GetMarketByID(ctx, marketID)
	if err != nil {
		tracing.RecordError(span, err)
		if blocked && errors.Is(err, serviceErrors.ErrMarketUnavailable) {
			s.logger.Warn(ctx, "Market is blocked locally and recheck failed, failing closed",
				zap.String("market_id", marketID.String()),
				zap.Error(err),
			)
		}

		return err
	}

	span.SetAttributes(
		attributes.MarketEnabledValue(market.Enabled),
		attributes.MarketDeletedValue(market.DeletedAt != nil),
		attributes.MarketBlockedValue(blocked),
	)

	if market.DeletedAt != nil {
		s.syncMarketBlock(ctx, market, true, "warm_block_after_deleted_recheck")

		err = sharedErrors.ErrMarketNotFound{ID: marketID}
		tracing.RecordError(span, err)
		return err
	}

	if !market.Enabled {
		s.syncMarketBlock(ctx, market, true, "warm_block_after_disabled_recheck")

		err = serviceErrors.ErrDisabled{ID: marketID}
		tracing.RecordError(span, err)
		return err
	}

	if blocked {
		s.syncMarketBlock(ctx, market, false, "remove_stale_block_after_recheck")
	}

	return nil
}

func (s *OrderService) getMarketBlockedState(
	ctx context.Context,
	span trace.Span,
	marketID uuid.UUID,
) (bool, error) {
	blocked, err := s.blockStore.IsBlocked(ctx, marketID)
	if err == nil {
		return blocked, nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		tracing.RecordError(span, err)
		return false, err
	}

	tracing.RecordError(span, err)
	metrics.CacheFallbacksTotal.
		WithLabelValues(s.config.ServiceName, "market_is_blocked", "lookup_error").
		Inc()

	s.logger.Warn(ctx, "Market block store lookup failed, falling back to market validation",
		zap.String("market_id", marketID.String()),
		zap.Error(err),
	)

	return blocked, nil
}

func (s *OrderService) syncMarketBlock(
	ctx context.Context,
	market sharedModels.Market,
	blocked bool,
	reason string,
) {
	updated, err := s.blockStore.SyncState(ctx, market.ID, blocked, market.UpdatedAt)
	if err != nil {
		metrics.MarketBlockStateSyncTotal.
			WithLabelValues(s.config.ServiceName, reason, strconv.FormatBool(blocked), "error", "false").
			Inc()

		s.logger.Warn(ctx, "Failed to sync market block state after recheck",
			zap.String("market_id", market.ID.String()),
			zap.Bool("blocked", blocked),
			zap.String("reason", reason),
			zap.Error(err),
		)
		return
	}

	metrics.MarketBlockStateSyncTotal.
		WithLabelValues(s.config.ServiceName, reason, strconv.FormatBool(blocked), "success", strconv.FormatBool(updated)).
		Inc()

	if updated {
		s.logger.Warn(ctx, "Synced market block state after recheck",
			zap.String("market_id", market.ID.String()),
			zap.Bool("blocked", blocked),
			zap.String("reason", reason),
		)
	}
}

// saveOrder сохраняет заказ и пишет OrderCreatedEvent в outbox в одной транзакции
func (s *OrderService) saveOrder(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
) (uuid.UUID, orderModel.OrderStatus, error) {
	const op = "OrderService.saveOrder"

	ctx, span := tracing.StartSpan(ctx, "order.save_order")
	defer span.End()

	now := time.Now().UTC()

	order := buildOrder(userID, marketID, orderType, price, quantity, now)
	event := buildOrderCreatedEvent(order, now)

	transaction, err := s.transactionManager.Begin(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: begin transaction: %w", op, err)
	}

	committed := false
	defer func() {
		if !committed {
			rollbackTransaction(ctx, transaction, s.logger, op, s.config.Timeout)
		}
	}()

	if err = s.saver.SaveOrder(ctx, transaction, order); err != nil {
		tracing.RecordError(span, err)
		if errors.Is(err, repositoryErrors.ErrOrderAlreadyExists) {
			return uuid.Nil, orderModel.OrderStatusUnspecified, sharedErrors.ErrAlreadyExists{ID: order.ID}
		}

		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err = s.eventProducer.ProduceOrderCreated(ctx, transaction, event); err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err = transaction.Commit(ctx); err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: commit transaction: %w", op, err)
	}

	committed = true
	metrics.OrdersCreatedTotal.WithLabelValues(s.config.ServiceName, marketID.String()).Inc()

	return order.ID, order.Status, nil
}

func rollbackTransaction(ctx context.Context,
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
	now time.Time,
) models.OrderCreatedEvent {
	return models.OrderCreatedEvent{
		EventID:   uuid.New(),
		OrderID:   order.ID,
		UserID:    order.UserID,
		MarketID:  order.MarketID,
		Type:      order.Type,
		Price:     order.Price,
		Quantity:  order.Quantity,
		Status:    order.Status,
		CreatedAt: now,
	}
}
