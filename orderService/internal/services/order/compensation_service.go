package order

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketInboxWriter interface {
	BeginProcessing(ctx context.Context, transaction pgx.Tx, event models.InboxEvent) (bool, models.InboxEventStatus, error)
	MarkProcessed(ctx context.Context, transaction pgx.Tx, eventID uuid.UUID, consumerGroup string) error
	SaveFailed(ctx context.Context, event models.InboxEvent, errText string) error
}

type MarketOrderCanceler interface {
	CancelActiveOrdersByMarket(ctx context.Context, transaction pgx.Tx, marketID uuid.UUID) ([]uuid.UUID, error)
}

type OrderEventProducer interface {
	ProduceOrderStatusUpdated(ctx context.Context, transaction pgx.Tx, event models.OrderStatusUpdatedEvent) error
}

type CompensationService struct {
	transactionManager TransactionManager
	inboxStore         MarketInboxWriter
	orderStore         MarketOrderCanceler
	blockStore         MarketBlockStore
	eventProducer      OrderEventProducer
	logger             *zapLogger.Logger
	config             config.OrderConfig
}

func NewCompensationService(
	manager TransactionManager,
	inboxWriter MarketInboxWriter,
	orderStore MarketOrderCanceler,
	blockStore MarketBlockStore,
	producer OrderEventProducer,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *CompensationService {
	return &CompensationService{
		transactionManager: manager,
		inboxStore:         inboxWriter,
		orderStore:         orderStore,
		blockStore:         blockStore,
		eventProducer:      producer,
		logger:             logger,
		config:             cfg,
	}
}

func (s *CompensationService) ProcessMarketStateChanged(
	ctx context.Context,
	topic string,
	consumerGroup string,
	rawPayload []byte,
	event sharedModels.MarketStateChangedEvent,
) error {
	const op = "MarketCompensationService.ProcessMarketStateChanged"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeouts.Service)
		defer cancel()
	}

	ctx, span := tracing.StartSpan(ctx, "market_compensation.process",
		trace.WithAttributes(
			attribute.String("event_id", event.EventID.String()),
			attribute.String("market_id", event.MarketID.String()),
			attribute.String("topic", topic),
			attribute.String("consumer_group", consumerGroup),
			attribute.Bool("market_enabled", event.Enabled),
			attribute.Bool("market_deleted", event.DeletedAt != nil),
		),
	)
	defer span.End()

	transaction, err := s.transactionManager.Begin(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: begin transaction: %w", op, err)
	}

	transactionClosed := false
	defer func() {
		if transactionClosed {
			return
		}
		RollbackTx(ctx, transaction, s.logger, "Market compensation transaction rollback failed", s.config.Timeouts.Service)
	}()

	inboxEvent := models.InboxEvent{
		ID:            uuid.New(),
		EventID:       event.EventID,
		Topic:         topic,
		ConsumerGroup: consumerGroup,
		Payload:       rawPayload,
		Status:        models.InboxEventStatusProcessing,
	}

	shouldProcess, currentStatus, err := s.inboxStore.BeginProcessing(ctx, transaction, inboxEvent)
	if err != nil {
		tracing.RecordError(span, err)
		transactionClosed = true
		return s.failProcessing(ctx, transaction, op, inboxEvent, err)
	}

	if !shouldProcess {
		transactionClosed, err = s.handleSkippedEvent(ctx, span, transaction, event, consumerGroup, currentStatus)
		if err != nil {
			return err
		}
		return nil
	}

	if err = s.applyCompensationTransaction(ctx, span, transaction, event); err != nil {
		tracing.RecordError(span, err)
		transactionClosed = true
		return s.failProcessing(ctx, transaction, op, inboxEvent, err)
	}

	if err = s.inboxStore.MarkProcessed(ctx, transaction, event.EventID, consumerGroup); err != nil {
		tracing.RecordError(span, err)
		transactionClosed = true
		return s.failProcessing(ctx, transaction, op, inboxEvent, err)
	}

	if err = transaction.Commit(ctx); err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: commit transaction: %w", op, err)
	}
	transactionClosed = true

	if err = s.syncMarketBlockState(ctx, event); err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: sync market block state after commit: %w", op, err)
	}

	return nil
}

func (s *CompensationService) handleSkippedEvent(
	ctx context.Context,
	span trace.Span,
	transaction pgx.Tx,
	event sharedModels.MarketStateChangedEvent,
	consumerGroup string,
	currentStatus models.InboxEventStatus,
) (bool, error) {
	s.logSkippedEvent(ctx, event, consumerGroup, currentStatus)

	if err := transaction.Commit(ctx); err != nil {
		tracing.RecordError(span, err)
		return false, fmt.Errorf("commit skipped transaction: %w", err)
	}

	if currentStatus == models.InboxEventStatusProcessed {
		if err := s.syncMarketBlockState(ctx, event); err != nil {
			tracing.RecordError(span, err)
			return false, fmt.Errorf("resync market block state for processed event: %w", err)
		}
	}

	return true, nil
}

func (s *CompensationService) applyCompensationTransaction(
	ctx context.Context,
	span trace.Span,
	transaction pgx.Tx,
	event sharedModels.MarketStateChangedEvent,
) error {
	if event.Enabled && event.DeletedAt == nil {
		return nil
	}

	cancelledIDs, err := s.orderStore.CancelActiveOrdersByMarket(ctx, transaction, event.MarketID)
	if err != nil {
		return err
	}

	if err = s.publishCancelledOrderEvents(ctx, transaction, event, cancelledIDs); err != nil {
		return err
	}

	span.SetAttributes(attribute.Int("orders_cancelled_count", len(cancelledIDs)))

	return nil
}

func (s *CompensationService) failProcessing(
	ctx context.Context,
	transaction pgx.Tx,
	op string,
	inboxEvent models.InboxEvent,
	processErr error,
) error {
	RollbackTx(ctx, transaction, s.logger, "Market compensation transaction failed", s.config.Timeouts.Service)

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.Timeouts.Service)
	defer cancel()

	if saveErr := s.inboxStore.SaveFailed(saveCtx, inboxEvent, processErr.Error()); saveErr != nil {
		s.logger.Error(saveCtx, "Failed to persist failed inbox event",
			zap.String("event_id", inboxEvent.EventID.String()),
			zap.String("consumer_group", inboxEvent.ConsumerGroup),
			zap.Error(saveErr),
		)
		return fmt.Errorf("%s: %w (additionally failed to persist inbox failure: %v)", op, processErr, saveErr)
	}

	return fmt.Errorf("%s: %w", op, processErr)
}

func (s *CompensationService) logSkippedEvent(
	ctx context.Context,
	event sharedModels.MarketStateChangedEvent,
	consumerGroup string,
	status models.InboxEventStatus,
) {
	message := "Duplicate market state changed event skipped"
	if status == models.InboxEventStatusProcessing {
		message = "Market state changed event is already being processed"
	}

	s.logger.Info(ctx, message,
		zap.String("event_id", event.EventID.String()),
		zap.String("market_id", event.MarketID.String()),
		zap.String("consumer_group", consumerGroup),
		zap.String("inbox_status", string(status)),
	)
}

func (s *CompensationService) publishCancelledOrderEvents(
	ctx context.Context,
	transaction pgx.Tx,
	marketEvent sharedModels.MarketStateChangedEvent,
	orderIDs []uuid.UUID,
) error {
	for _, orderID := range orderIDs {
		causationID := marketEvent.EventID

		statusEvent := models.OrderStatusUpdatedEvent{
			EventID:       uuid.New(),
			OrderID:       orderID,
			NewStatus:     shared.OrderStatusCancelled,
			Reason:        "market became unavailable",
			CorrelationID: marketEvent.CorrelationID,
			CausationID:   &causationID,
			UpdatedAt:     time.Now().UTC(),
		}

		if err := s.eventProducer.ProduceOrderStatusUpdated(ctx, transaction, statusEvent); err != nil {
			return fmt.Errorf("publish cancelled order status event for order %s: %w", orderID, err)
		}
	}

	return nil
}

func (s *CompensationService) syncMarketBlockState(
	ctx context.Context,
	event sharedModels.MarketStateChangedEvent,
) error {
	blocked := !event.Enabled || event.DeletedAt != nil

	return s.blockStore.SyncState(ctx, event.MarketID, blocked, event.UpdatedAt)
}
