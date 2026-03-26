package producer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
	mapper "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/kafka"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
)

type OutboxWriter interface {
	SaveOutboxEvent(ctx context.Context, event models.OutboxEvent) error
}

type MarketProducer struct {
	outboxWriter OutboxWriter
	logger       *zapLogger.Logger
}

func New(writer OutboxWriter, logger *zapLogger.Logger) *MarketProducer {
	return &MarketProducer{
		outboxWriter: writer,
		logger:       logger,
	}
}

// ProduceMarketStateChanged не публикует напрямую в Kafka - это делает outbox.Worker асинхронно.
func (p *MarketProducer) ProduceMarketStateChanged(
	ctx context.Context,
	event sharedModels.MarketStateChangedEvent,
) error {
	const op = "MarketProducer.ProduceMarketStateChanged"

	ctx, span := tracing.StartSpan(ctx, "producer.produce_market_state_changed")
	defer span.End()

	outboxEvent, err := p.buildOutboxEvent(ctx, span, event)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err = p.saveOutboxEvent(ctx, span, event, outboxEvent); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	p.logger.Info(ctx, "MarketStateChangesEvent prepared for outbox saving",
		zap.String("market_id", event.MarketID.String()),
		zap.String("event_id", event.EventID.String()),
		zap.String("outbox_event_id", outboxEvent.ID.String()),
	)

	return nil
}

func (p *MarketProducer) buildOutboxEvent(
	ctx context.Context,
	span trace.Span,
	event sharedModels.MarketStateChangedEvent,
) (models.OutboxEvent, error) {
	payload, err := mapper.Marshal(event)
	if err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to marshal MarketStateChangedEvent",
			zap.String("market_id", event.MarketID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err),
		)

		return models.OutboxEvent{}, err
	}

	return models.OutboxEvent{
		ID:          uuid.New(),
		EventID:     event.EventID,
		EventType:   sharedModels.MarketStateChangedEventType,
		AggregateID: event.MarketID,
		Payload:     payload,
		Status:      models.OutboxEventStatusPending,
	}, nil
}

func (p *MarketProducer) saveOutboxEvent(
	ctx context.Context,
	span trace.Span,
	event sharedModels.MarketStateChangedEvent,
	outboxEvent models.OutboxEvent,
) error {
	if err := p.outboxWriter.SaveOutboxEvent(ctx, outboxEvent); err != nil {
		tracing.RecordError(span, err)
		p.logger.Error(ctx, "Failed to save market event to outbox",
			zap.String("market_id", event.MarketID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err))

		return err
	}

	return nil
}
