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
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
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
	eventProducer      EventProducer
	idempotencyService *IdempotencyService
	logger             *zapLogger.Logger
	config             config.OrderConfig
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
	SynchronizeState(ctx context.Context, marketID uuid.UUID, blocked bool, updatedAt time.Time) (bool, error)
	IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error)
}

type Getter interface {
	GetOrder(ctx context.Context, id, userID uuid.UUID) (models.Order, error)
	FindOrderForIdempotencyRecovery(ctx context.Context, userID, marketID uuid.UUID,
		orderType orderModel.OrderType, price orderModel.Decimal, quantity int64, startedAt time.Time,
	) (models.Order, error)
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
	producer EventProducer,
	service *IdempotencyService,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *OrderService {
	return &OrderService{
		transactionManager: manager,
		saver:              saver,
		getter:             getter,
		marketViewer:       viewer,
		blockStore:         store,
		rateLimiters:       limiters,
		eventProducer:      producer,
		idempotencyService: service,
		logger:             logger,
		config:             cfg,
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

	ctx, cancel := contextWithTimeout(ctx, s.config.Timeouts.Service)
	defer cancel()

	requestHash := s.idempotencyService.buildRequestHash(marketID, orderType, price, quantity)

	idemResult, acquired, idemError := s.idempotencyService.acquire(ctx, userID, requestHash)
	if idemError != nil {
		s.logger.Warn(ctx, "idempotency acquire failed",
			zap.Error(idemError),
		)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, idemError)
	}
	if !acquired {
		return s.resolveIdempotentRequest(
			ctx,
			userID,
			marketID,
			orderType,
			price,
			quantity,
			requestHash,
			idemResult,
		)
	}

	if err := s.checkRateLimit(ctx, userID, s.rateLimiters.Create, "create_order"); err != nil {
		s.idempotencyService.failCleanup(ctx, userID, requestHash, acquired)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	if err := s.validateMarket(ctx, marketID); err != nil {
		s.idempotencyService.failCleanup(ctx, userID, requestHash, acquired)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	orderID, orderStatus, err := s.saveOrder(ctx, userID, marketID, orderType, price, quantity)
	if err != nil {
		s.idempotencyService.failCleanup(ctx, userID, requestHash, acquired)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, err)
	}

	// Ошибку не нужно возвращать, так как ордер уже закоммичен и ретрай клиента не нужен
	if err = s.completeIdempotencySync(ctx, userID, orderID, requestHash, orderStatus); err != nil {
		s.logger.Error(ctx, "order committed but idempotency completion failed",
			zap.String("order_id", orderID.String()),
			zap.String("request_hash", requestHash),
			zap.Error(err),
		)
	}
	return orderID, orderStatus, nil
}

func (s *OrderService) GetOrderStatus(
	ctx context.Context,
	orderID, userID uuid.UUID,
) (orderModel.OrderStatus, error) {
	const op = "OrderService.GetOrderStatus"

	ctx, cancel := contextWithTimeout(ctx, s.config.Timeouts.Service)
	defer cancel()

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

func (s *OrderService) resolveIdempotentRequest(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
	requestHash string,
	idemResult IdempotencyResult,
) (uuid.UUID, orderModel.OrderStatus, error) {
	if idemResult.IsCompleted {
		return s.idempotencyService.checkIdempotencyResult(ctx, idemResult)
	}

	if !idemResult.IsProcessing {
		return uuid.Nil, orderModel.OrderStatusUnspecified, errors.New("unknown idempotency state")
	}

	return s.tryRecoverOrderFromProcessing(ctx, userID, marketID, orderType, price, quantity, requestHash, idemResult)
}

func (s *OrderService) tryRecoverOrderFromProcessing(
	ctx context.Context,
	userID uuid.UUID,
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
	requestHash string,
	idemResult IdempotencyResult,
) (uuid.UUID, orderModel.OrderStatus, error) {
	if idemResult.StartedAt.IsZero() {
		return uuid.Nil, orderModel.OrderStatusUnspecified, serviceErrors.ErrOrderProcessing
	}

	order, err := s.getter.FindOrderForIdempotencyRecovery(ctx, userID, marketID, orderType,
		price, quantity, idemResult.StartedAt)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrOrderNotFound) {
			return uuid.Nil, orderModel.OrderStatusUnspecified, serviceErrors.ErrOrderProcessing
		}
		return uuid.Nil, orderModel.OrderStatusUnspecified, err
	}

	if err = s.completeIdempotencySync(ctx, userID, order.ID, requestHash, order.Status); err != nil {
		s.logger.Warn(ctx, "recovered order but failed to finalize idempotency state",
			zap.String("order_id", order.ID.String()),
			zap.String("request_hash", requestHash),
			zap.Error(err),
		)
	}

	s.logger.Info(ctx, "recovered order from processing idempotency state",
		zap.String("order_id", order.ID.String()),
		zap.String("request_hash", requestHash),
	)

	return order.ID, order.Status, nil
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
		metrics.RateLimitRejectedBusinessTotal.WithLabelValues(s.config.Service.Name, operation).Inc()
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

	// Обновление кэша происходит асинхронно
	if market.DeletedAt != nil {
		s.synchronizeMarketBlockAsync(ctx, market, true, "warm_block_after_deleted_recheck")

		err = sharedErrors.ErrMarketNotFound{ID: marketID}
		tracing.RecordError(span, err)
		return err
	}

	if !market.Enabled {
		s.synchronizeMarketBlockAsync(ctx, market, true, "warm_block_after_disabled_recheck")

		err = serviceErrors.ErrDisabled{ID: marketID}
		tracing.RecordError(span, err)
		return err
	}

	if blocked {
		s.synchronizeMarketBlockAsync(ctx, market, false, "remove_stale_block_after_recheck")
	}

	return nil
}

func (s *OrderService) synchronizeMarketBlockAsync(
	ctx context.Context,
	market sharedModels.Market,
	blocked bool,
	reason string,
) {
	go func() {
		syncCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			s.config.Timeouts.Service,
		)
		defer cancel()

		_ = recovery.PanicRecoveryHandler(
			syncCtx,
			s.logger,
			"order.synchronize_market_block_async",
			func() error {
				s.synchronizeMarketBlock(syncCtx, market, blocked, reason)
				return nil
			},
		)
	}()
}

func (s *OrderService) getMarketBlockedState(
	ctx context.Context,
	span trace.Span,
	marketID uuid.UUID,
) (bool, error) {
	// Получение статуса происходит синхронно
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
		WithLabelValues(s.config.Service.Name, "market_is_blocked", "lookup_error").
		Inc()

	s.logger.Warn(ctx, "Market block store lookup failed, falling back to market validation",
		zap.String("market_id", marketID.String()),
		zap.Error(err),
	)

	return blocked, nil
}

func (s *OrderService) synchronizeMarketBlock(
	ctx context.Context,
	market sharedModels.Market,
	blocked bool,
	reason string,
) {
	updated, err := s.blockStore.SynchronizeState(ctx, market.ID, blocked, market.UpdatedAt)
	if err != nil {
		metrics.MarketBlockStateSyncTotal.
			WithLabelValues(s.config.Service.Name, reason, strconv.FormatBool(blocked), "error", "false").
			Inc()

		s.logger.Warn(ctx, "Failed to synchronize market block state after recheck",
			zap.String("market_id", market.ID.String()),
			zap.Bool("blocked", blocked),
			zap.String("reason", reason),
			zap.Error(err),
		)
		return
	}

	metrics.MarketBlockStateSyncTotal.
		WithLabelValues(s.config.Service.Name, reason, strconv.FormatBool(blocked), "success", strconv.FormatBool(updated)).
		Inc()

	if updated {
		s.logger.Info(ctx, "Synchronized market block state after recheck",
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
			rollbackTransaction(ctx, transaction, s.logger, op, s.config.Timeouts.Service)
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

	if err = commitTransaction(ctx, transaction, s.config.Timeouts.Service); err != nil {
		tracing.RecordError(span, err)
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: commit transaction: %w", op, err)
	}

	committed = true
	metrics.OrdersCreatedTotal.WithLabelValues(s.config.Service.Name, marketID.String()).Inc()

	return order.ID, order.Status, nil
}

func (s *OrderService) completeIdempotencySync(
	ctx context.Context,
	userID, orderID uuid.UUID,
	requestHash string,
	orderStatus orderModel.OrderStatus,
) error {
	attempts := s.config.Redis.Idempotency.CompleteAttempts
	attemptTimeout := s.config.Redis.Idempotency.CompleteAttemptTimeout
	retryDelay := s.config.Redis.Idempotency.CompleteRetryDelay

	totalTimeout := time.Duration(attempts) * attemptTimeout
	if attempts > 1 {
		totalTimeout += time.Duration(attempts-1) * retryDelay
	}

	completeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), totalTimeout)
	defer cancel()

	return recovery.PanicRecoveryHandler(
		completeCtx,
		s.logger,
		"order.complete_idempotency_sync",
		func() error {
			return s.idempotencyService.completeIdempotencyChecking(
				completeCtx,
				userID,
				orderID,
				requestHash,
				orderStatus,
			)
		},
	)
}

func rollbackTransaction(
	ctx context.Context,
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

func commitTransaction(
	ctx context.Context,
	transaction pgx.Tx,
	timeout time.Duration,
) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	return transaction.Commit(commitCtx)
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

func contextWithTimeout(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	if timeout <= 0 {
		return ctx, func() {}
	}

	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return ctx, func() {}
		}
	}

	return context.WithTimeout(ctx, timeout)
}
