package consumer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/inbound/kafka"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketEventProcessor interface {
	ProcessMarketStateChanged(
		ctx context.Context,
		topic string,
		consumerGroup string,
		rawPayload []byte,
		event models.MarketStateChangedEvent,
	) error
}

type MarketConsumer struct {
	consumer      Consumer
	processor     MarketEventProcessor
	consumerGroup string
	logger        *zapLogger.Logger
}

func NewMarketConsumer(
	consumer Consumer,
	processor MarketEventProcessor,
	consumerGroup string,
	logger *zapLogger.Logger,
) *MarketConsumer {
	return &MarketConsumer{
		consumer:      consumer,
		processor:     processor,
		consumerGroup: consumerGroup,
		logger:        logger,
	}
}

func (c *MarketConsumer) Run(ctx context.Context) error {
	return c.consumer.Consume(ctx, c.handleMarketStateChanged)
}

func (c *MarketConsumer) handleMarketStateChanged(ctx context.Context, msg kafka.Message) error {
	const op = "MarketConsumer.handleMarketStateChanged"

	ctx, span := tracing.StartSpan(ctx, "market_consumer.handle_market_state_changed",
		trace.WithAttributes(
			attribute.String("messaging.system", messagingSystem),
			attribute.String("messaging.destination", msg.Topic),
			attribute.Int64("messaging.kafka.offset", msg.Offset),
			attribute.Int("messaging.message_payload_size_bytes", len(msg.Value)),
		),
	)
	defer span.End()

	event, err := mapper.UnmarshalMarketStateChanged(msg.Value)
	if err != nil {
		tracing.RecordError(span, err)

		c.logger.Error(ctx, "Failed to unmarshal MarketStateChangedEvent",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
			zap.Error(err),
		)

		return fmt.Errorf("%s: %w", op, err)
	}

	span.SetAttributes(
		attribute.String("event_id", event.EventID.String()),
		attribute.String("market_id", event.MarketID.String()),
		attribute.Bool("market_enabled", event.Enabled),
		attribute.Bool("market_deleted", event.DeletedAt != nil),
	)

	if err = c.processor.ProcessMarketStateChanged(
		ctx,
		msg.Topic,
		c.consumerGroup,
		msg.Value,
		event,
	); err != nil {
		tracing.RecordError(span, err)

		c.logger.Error(ctx, "Failed to process market state changed event",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
			zap.String("event_id", event.EventID.String()),
			zap.String("market_id", event.MarketID.String()),
			zap.Error(err),
		)

		return fmt.Errorf("%s: %w", op, err)
	}

	c.logger.Info(ctx, "Market state changed event processed",
		zap.String("topic", msg.Topic),
		zap.Int32("partition", msg.Partition),
		zap.Int64("offset", msg.Offset),
		zap.String("event_id", event.EventID.String()),
		zap.String("market_id", event.MarketID.String()),
	)

	return nil
}
