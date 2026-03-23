package order

import (
	"context"
	"errors"
	"fmt"

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
)

type TransactionManager interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type InboxWriter interface {
	BeginProcessing(ctx context.Context, transaction pgx.Tx, event models.InboxEvent) (bool, error)
	MarkProcessed(ctx context.Context, transaction pgx.Tx, eventID uuid.UUID, consumerGroup string) error
	SaveFailed(ctx context.Context, event models.InboxEvent, errText string) error
}

type OrderStatusWriter interface {
	UpdateOrderStatus(ctx context.Context, transaction pgx.Tx, orderID uuid.UUID, status shared.OrderStatus) error
}

type SagaReplyService struct {
	transactionManager TransactionManager
	inboxStore         InboxWriter
	orderStatusStore   OrderStatusWriter
	logger             *zapLogger.Logger
	config             config.OrderConfig
}

func NewSagaReplyService(
	manager TransactionManager,
	writer InboxWriter,
	statusWriter OrderStatusWriter,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *SagaReplyService {
	return &SagaReplyService{
		transactionManager: manager,
		inboxStore:         writer,
		orderStatusStore:   statusWriter,
		logger:             logger,
		config:             cfg,
	}
}

func (s *SagaReplyService) ProcessSagaReply(
	ctx context.Context,
	topic string,
	consumerGroup string,
	rawPayload []byte,
	event models.OrderStatusUpdatedEvent,
) error {
	const op = "SagaReplyService.ProcessSagaReply"

	ctx, span := tracing.StartSpan(ctx, "saga_reply.process",
		trace.WithAttributes(
			attribute.String("event_id", event.EventID.String()),
			attribute.String("order_id", event.OrderID.String()),
			attribute.String("saga_id", event.SagaID.String()),
			attribute.String("topic", topic),
			attribute.String("consumer_group", consumerGroup),
		),
	)
	defer span.End()

	transaction, err := s.transactionManager.Begin(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: begin transaction: %w", op, err)
	}
	defer func() {
		if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			s.logger.Error(ctx, "Saga reply transaction rollback failed", zap.Error(rollbackErr))
		}
	}()

	inboxEvent := models.InboxEvent{
		ID:            uuid.New(),
		EventID:       event.EventID,
		Topic:         topic,
		ConsumerGroup: consumerGroup,
		Payload:       rawPayload,
		Status:        models.InboxEventStatusProcessing,
	}

	started, err := s.inboxStore.BeginProcessing(ctx, transaction, inboxEvent)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: begin inbox processing: %w", op, err)
	}

	if !started {
		s.logger.Info(ctx, "Duplicate saga reply event skipped",
			zap.String("event_id", event.EventID.String()),
			zap.String("order_id", event.OrderID.String()),
			zap.String("consumer_group", consumerGroup),
		)
		return nil
	}

	if err = s.orderStatusStore.UpdateOrderStatus(ctx, transaction, event.OrderID, event.NewStatus); err != nil {
		tracing.RecordError(span, err)
		return s.failProcessing(ctx, op, transaction, inboxEvent, err)
	}

	if err = s.inboxStore.MarkProcessed(ctx, transaction, event.EventID, consumerGroup); err != nil {
		tracing.RecordError(span, err)
		return s.failProcessing(ctx, op, transaction, inboxEvent, err)
	}

	if err = transaction.Commit(ctx); err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: commit transaction: %w", op, err)
	}

	return nil
}

func (s *SagaReplyService) failProcessing(
	ctx context.Context,
	op string,
	transaction pgx.Tx,
	inboxEvent models.InboxEvent,
	processErr error,
) error {
	if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
		s.logger.Error(ctx, "Saga reply transaction rollback failed", zap.Error(rollbackErr))
	}

	if saveErr := s.inboxStore.SaveFailed(ctx, inboxEvent, processErr.Error()); saveErr != nil {
		s.logger.Error(ctx, "Failed to persist failed inbox event",
			zap.String("event_id", inboxEvent.EventID.String()),
			zap.String("consumer_group", inboxEvent.ConsumerGroup),
			zap.Error(saveErr),
		)
		return fmt.Errorf("%s: %w (additionally failed to persist inbox failure: %v)", op, processErr, saveErr)
	}

	return fmt.Errorf("%s: %w", op, processErr)
}
