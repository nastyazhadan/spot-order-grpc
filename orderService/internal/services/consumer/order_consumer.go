package consumer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/inbound/kafka"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/consumer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
)

const messagingSystem = "kafka"

type Consumer interface {
	Consume(ctx context.Context, handler consumer.MessageHandler) error
}

type SagaReplyProcessor interface {
	ProcessSagaReply(
		ctx context.Context,
		topic string,
		consumerGroup string,
		rawPayload []byte,
		event models.OrderStatusUpdatedEvent,
	) error
}
type OrderConsumer struct {
	consumer  Consumer
	processor SagaReplyProcessor
	logger    *zapLogger.Logger
	config    config.OrderConfig
}

func New(
	consumer Consumer,
	processor SagaReplyProcessor,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *OrderConsumer {
	return &OrderConsumer{
		consumer:  consumer,
		processor: processor,
		logger:    logger,
		config:    cfg,
	}
}

func (c *OrderConsumer) Run(ctx context.Context) error {
	return c.consumer.Consume(ctx, c.handleSagaReply)
}

func (c *OrderConsumer) handleSagaReply(ctx context.Context, msg kafka.Message) error {
	const op = "OrderConsumer.handleSagaReply"

	ctx, span := tracing.StartSpan(ctx, "order_consumer.handle_saga_reply",
		trace.WithAttributes(
			attribute.String("messaging.system", messagingSystem),
			attribute.String("messaging.destination", msg.Topic),
			attribute.Int64("messaging.kafka.offset", msg.Offset),
			attribute.Int("messaging.message_payload_size_bytes", len(msg.Value)),
		),
	)
	defer span.End()

	event, err := c.unmarshalOutboxEvent(ctx, span, msg)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	err = c.processor.ProcessSagaReply(
		ctx,
		msg.Topic,
		c.config.Kafka.Consumer.GroupID,
		msg.Value,
		event,
	)
	if err != nil {
		tracing.RecordError(span, err)
		c.logProcessingError(ctx, msg, event, err)

		return fmt.Errorf("%s: %w", op, err)
	}

	c.logProcessed(ctx, msg, event)

	return nil
}

func (c *OrderConsumer) unmarshalOutboxEvent(
	ctx context.Context,
	span trace.Span,
	msg kafka.Message,
) (models.OrderStatusUpdatedEvent, error) {
	event, err := mapper.Unmarshal(msg.Value)
	if err != nil {
		tracing.RecordError(span, err)

		c.logger.Error(ctx, "Failed to unmarshal OrderStatusUpdatedEvent",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
			zap.Error(err),
		)

		return models.OrderStatusUpdatedEvent{}, err
	}

	span.SetAttributes(
		attribute.String("event_id", event.EventID.String()),
		attribute.String("order_id", event.OrderID.String()),
		attribute.String("saga_id", event.SagaID.String()),
		attribute.String("correlation_id", event.CorrelationID.String()),
	)

	return event, nil
}

func (c *OrderConsumer) logProcessingError(
	ctx context.Context,
	msg kafka.Message,
	event models.OrderStatusUpdatedEvent,
	err error,
) {
	c.logger.Error(ctx, "Failed to process saga reply event",
		zap.String("topic", msg.Topic),
		zap.Int32("partition", msg.Partition),
		zap.Int64("offset", msg.Offset),
		zap.String("event_id", event.EventID.String()),
		zap.String("order_id", event.OrderID.String()),
		zap.String("saga_id", event.SagaID.String()),
		zap.Error(err),
	)
}

func (c *OrderConsumer) logProcessed(
	ctx context.Context,
	msg kafka.Message,
	event models.OrderStatusUpdatedEvent,
) {
	c.logger.Info(ctx, "Saga reply event processed",
		zap.String("topic", msg.Topic),
		zap.Int32("partition", msg.Partition),
		zap.Int64("offset", msg.Offset),
		zap.String("event_id", event.EventID.String()),
		zap.String("order_id", event.OrderID.String()),
		zap.String("saga_id", event.SagaID.String()),
	)
}
