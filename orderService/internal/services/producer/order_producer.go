package producer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	payload, err := mapper.MarshalOrderCreated(event)
	if err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to marshal OrderCreatedEvent",
			zap.String("order_id", event.OrderID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("%s: marshal OrderCreatedEvent: %w", op, err)
	}

	outboxEvent := p.buildOrderCreatedOutboxEvent(event, payload)

	if err = p.outboxWriter.SaveOutboxEvent(ctx, transaction, outboxEvent); err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to save OrderCreatedEvent to outbox",
			zap.String("order_id", event.OrderID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.String("outbox_event_id", outboxEvent.ID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("%s: save OrderCreatedEvent to outbox: %w", op, err)
	}

	p.logger.Info(ctx, "OrderCreatedEvent prepared for outbox saving",
		zap.String("order_id", event.OrderID.String()),
		zap.String("event_id", event.EventID.String()),
		zap.String("outbox_event_id", outboxEvent.ID.String()),
	)

	return nil
}

func (p *OrderProducer) ProduceOrderStatusUpdated(
	ctx context.Context,
	transaction pgx.Tx,
	event models.OrderStatusUpdatedEvent,
) error {
	const op = "OrderProducer.ProduceOrderStatusUpdated"

	ctx, span := tracing.StartSpan(ctx, "producer.produce_order_status_updated")
	defer span.End()

	payload, err := mapper.MarshalOrderStatusUpdated(event)
	if err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to marshal OrderStatusUpdatedEvent",
			zap.String("order_id", event.OrderID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("%s: marshal OrderStatusUpdatedEvent: %w", op, err)
	}

	outboxEvent := p.buildOrderStatusUpdatedOutboxEvent(event, payload)

	if err = p.outboxWriter.SaveOutboxEvent(ctx, transaction, outboxEvent); err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to save OrderStatusUpdatedEvent to outbox",
			zap.String("order_id", event.OrderID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.String("outbox_event_id", outboxEvent.ID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("%s: save OrderStatusUpdatedEvent to outbox: %w", op, err)
	}

	p.logger.Info(ctx, "OrderStatusUpdatedEvent prepared for outbox saving",
		zap.String("order_id", event.OrderID.String()),
		zap.String("event_id", event.EventID.String()),
		zap.String("outbox_event_id", outboxEvent.ID.String()),
		zap.Int32("new_status", int32(event.NewStatus)),
	)

	return nil
}

func (p *OrderProducer) buildOrderCreatedOutboxEvent(
	event models.OrderCreatedEvent,
	payload []byte,
) models.OutboxEvent {
	return models.OutboxEvent{
		ID:          uuid.New(),
		EventID:     event.EventID,
		EventType:   models.OrderCreatedEventType,
		AggregateID: event.OrderID,
		Payload:     payload,
		Status:      models.OutboxEventStatusPending,
	}
}

func (p *OrderProducer) buildOrderStatusUpdatedOutboxEvent(
	event models.OrderStatusUpdatedEvent,
	payload []byte,
) models.OutboxEvent {
	return models.OutboxEvent{
		ID:          uuid.New(),
		EventID:     event.EventID,
		EventType:   models.OrderStatusUpdatedEventType,
		AggregateID: event.OrderID,
		Payload:     payload,
		Status:      models.OutboxEventStatusPending,
	}
}
