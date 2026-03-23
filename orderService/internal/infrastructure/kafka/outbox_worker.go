package outbox

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/outbound/kafka"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

type EventStore interface {
	ClaimPendingEvents(ctx context.Context, limit int) ([]models.OutboxEvent, error)
	MarkPublished(ctx context.Context, event models.OutboxEvent) error
	ScheduleRetry(ctx context.Context, event models.OutboxEvent, nextRetryAt time.Time, errText string) error
	MarkFailed(ctx context.Context, event models.OutboxEvent, errText string) error
}

type EventPublisher interface {
	Send(ctx context.Context, key, value []byte) error
}

type Worker struct {
	store        EventStore
	publisher    EventPublisher
	pollInterval time.Duration
	batchSize    int
	maxRetries   int
	logger       *zapLogger.Logger
	cfg          config.OrderConfig
}

func NewWorker(
	store EventStore,
	publisher EventPublisher,
	pollInterval time.Duration,
	batchSize int,
	maxRetries int,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *Worker {
	return &Worker{
		store:        store,
		publisher:    publisher,
		pollInterval: pollInterval,
		batchSize:    batchSize,
		maxRetries:   maxRetries,
		logger:       logger,
		cfg:          cfg,
	}
}

// Run запускает цикл опроса outbox. Блокирует до отмены ctx.
// Запускается в отдельной горутине из Fx OnStart
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info(ctx, "Outbox worker started",
		zap.Duration("poll_interval", w.pollInterval),
		zap.Int("batch_size", w.batchSize),
		zap.Int("max_retries", w.maxRetries),
	)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info(ctx, "Outbox worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	ctx, span := tracing.StartSpan(ctx, "outbox.worker.process_batch",
		trace.WithAttributes(attribute.Int("batch.max_size", w.batchSize)),
	)
	defer span.End()

	start := time.Now()
	events, err := w.store.ClaimPendingEvents(ctx, w.batchSize)
	if err != nil {
		tracing.RecordError(span, err)
		w.logger.Error(ctx, "Failed to fetch outbox events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	span.SetAttributes(attribute.Int("batch.fetched", len(events)))

	for _, event := range events {
		w.processEvent(ctx, event)
	}

	elapsed := time.Since(start).Seconds()
	metrics.ObserveWithTrace(ctx,
		metrics.OutboxWorkerDuration.WithLabelValues(w.cfg.Service.Name),
		elapsed,
	)
}

func (w *Worker) processEvent(ctx context.Context, event models.OutboxEvent) {
	ctx, span := tracing.StartSpan(ctx, "outbox.worker.publish_event",
		trace.WithAttributes(
			attribute.String("event_type", event.EventType),
			attribute.String("aggregate_id", event.AggregateID.String()),
			attribute.Int("retry_count", event.RetryCount),
		),
	)
	defer span.End()

	key := w.messageKey(event)

	if err := w.publisher.Send(ctx, key, event.Payload); err != nil {
		tracing.RecordError(span, err)

		nextRetryCount := event.RetryCount + 1
		errText := err.Error()

		if nextRetryCount >= w.maxRetries {
			w.logger.Error(ctx, "Outbox event exhausted retries, marking as failed",
				zap.String("event_type", event.EventType),
				zap.String("aggregate_id", event.AggregateID.String()),
				zap.Int("retry_count", event.RetryCount),
				zap.Error(err),
			)

			if markErr := w.store.MarkFailed(ctx, event, errText); markErr != nil {
				w.logger.Error(ctx, "Failed to mark outbox event as failed",
					zap.String("outbox_id", event.ID.String()),
					zap.Error(markErr),
				)
			}
			return
		}

		backoff := time.Duration(nextRetryCount) * w.pollInterval
		nextRetryAt := time.Now().UTC().Add(backoff)

		if retryErr := w.store.ScheduleRetry(ctx, event, nextRetryAt, errText); retryErr != nil {
			w.logger.Error(ctx, "Failed to schedule retry for outbox event",
				zap.String("outbox_id", event.ID.String()),
				zap.Error(retryErr),
			)
			return
		}

		w.logger.Warn(ctx, "Failed to publish outbox event, scheduled retry",
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.Int("retry_count", nextRetryCount),
			zap.Time("next_retry_at", nextRetryAt),
			zap.Error(err),
		)
		return
	}

	if err := w.store.MarkPublished(ctx, event); err != nil {
		// Здесь может возникнуть дублирование
		w.logger.Error(ctx, "Published to Kafka but failed to mark outbox event as published — possible duplicate",
			zap.String("outbox_id", event.ID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err),
		)
		tracing.RecordError(span, err)
		return
	}

	metrics.OutboxEventsTotal.WithLabelValues(w.cfg.Service.Name, event.EventType, "published").Inc()

	w.logger.Info(ctx, "Outbox event published",
		zap.String("event_type", event.EventType),
		zap.String("aggregate_id", event.AggregateID.String()),
		zap.String("event_id", event.EventID.String()),
	)
}

func (w *Worker) messageKey(event models.OutboxEvent) []byte {
	switch event.EventType {
	case models.OrderCreatedEventType:
		return kafka.OrderCreatedKey(event.AggregateID)
	default:
		return []byte(event.AggregateID.String())
	}
}
