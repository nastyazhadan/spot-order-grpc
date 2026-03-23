package producer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/outbound/kafka"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
)

type OutboxWriter interface {
	SaveOutboxEvent(ctx context.Context, transaction pgx.Tx, event models.OutboxEvent) error
}

type OrderProducer struct {
	outboxWriter OutboxWriter
	logger       *zapLogger.Logger
}

func New(writer OutboxWriter, logger *zapLogger.Logger) *OrderProducer {
	return &OrderProducer{
		outboxWriter: writer,
		logger:       logger,
	}
}

// ProduceOrderCreated не публикует напрямую в Kafka - это делает outbox.Worker асинхронно.
// Атомарность гарантируется тем, что SaveOrder и SaveOutboxEvent выполняются
// в одной транзакции: если транзакция откатывается, событие не сохраняется
func (p *OrderProducer) ProduceOrderCreated(
	ctx context.Context,
	transaction pgx.Tx,
	event models.OrderCreatedEvent,
) error {
	const op = "OrderProducer.ProduceOrderCreated"

	ctx, span := tracing.StartSpan(ctx, "producer.produce_order_created")
	defer span.End()

	outboxEvent, err := p.buildOutboxEvent(ctx, span, event)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err = p.saveOutboxEvent(ctx, span, transaction, event, outboxEvent); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	p.logger.Info(ctx, "OrderCreatedEvent saved to outbox",
		zap.String("order_id", event.OrderID.String()),
		zap.String("event_id", event.EventID.String()),
		zap.String("outbox_event_id", outboxEvent.ID.String()),
	)

	return nil
}

func (p *OrderProducer) buildOutboxEvent(
	ctx context.Context,
	span trace.Span,
	event models.OrderCreatedEvent,
) (models.OutboxEvent, error) {
	payload, err := mapper.Marshal(event)
	if err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to marshal OrderCreatedEvent",
			zap.String("order_id", event.OrderID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err))

		return models.OutboxEvent{}, err
	}

	return models.OutboxEvent{
		ID:          uuid.New(),
		EventID:     event.EventID,
		EventType:   models.OrderCreatedEventType,
		AggregateID: event.OrderID,
		Payload:     payload,
		Status:      models.OutboxEventStatusPending,
	}, nil
}

func (p *OrderProducer) saveOutboxEvent(
	ctx context.Context,
	span trace.Span,
	transaction pgx.Tx,
	event models.OrderCreatedEvent,
	outboxEvent models.OutboxEvent,
) error {
	if err := p.outboxWriter.SaveOutboxEvent(ctx, transaction, outboxEvent); err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to save event to outbox",
			zap.String("order_id", event.OrderID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err))

		return err
	}

	return nil
}
