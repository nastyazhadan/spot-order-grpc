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
	MarkFailed(ctx context.Context, event models.OutboxEvent, errText string) error
	ScheduleRetry(ctx context.Context, event models.OutboxEvent, nextRetryAt time.Time, errText string) error
	ReleaseStuckEvents(ctx context.Context, stuckBefore time.Time) (int64, error)
}

type EventPublisher interface {
	Send(ctx context.Context, key, value []byte) error
}

type Worker struct {
	store        EventStore
	publisher    EventPublisher
	pollInterval time.Duration
	batchSize    int
	batchTimeout time.Duration
	maxRetries   int
	logger       *zapLogger.Logger
	cfg          config.OrderConfig
}

func NewWorker(
	store EventStore,
	publisher EventPublisher,
	pollInterval time.Duration,
	batchSize int,
	timeout time.Duration,
	maxRetries int,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *Worker {
	return &Worker{
		store:        store,
		publisher:    publisher,
		pollInterval: pollInterval,
		batchSize:    batchSize,
		batchTimeout: timeout,
		maxRetries:   maxRetries,
		logger:       logger,
		cfg:          cfg,
	}
}

func (w *Worker) newOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), w.batchTimeout)
}

// Run запускает цикл опроса outbox. Блокирует до отмены ctx.
// Запускается в отдельной горутине из Fx OnStart
// Работают два независимых тикера:
//   - pollTicker (pollInterval) — основной цикл публикации pending-событий
//   - cleanupTicker (processingTimeout) — сброс застрявших processing-событий в pending
func (w *Worker) Run(ctx context.Context) {
	processingTimeout := w.cfg.Kafka.Outbox.ProcessingTimeout

	w.logger.Info(ctx, "Outbox worker started",
		zap.Duration("poll_interval", w.pollInterval),
		zap.Int("batch_size", w.batchSize),
		zap.Int("max_retries", w.maxRetries),
		zap.Duration("processing_timeout", processingTimeout),
	)

	pollTicker := time.NewTicker(w.pollInterval)
	defer pollTicker.Stop()

	cleanUpTicker := time.NewTicker(processingTimeout)
	defer cleanUpTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info(ctx, "Outbox worker stopped")
			return
		case <-pollTicker.C:
			w.processBatch(ctx)
		case <-cleanUpTicker.C:
			w.releaseStuck(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	claimCtx, cancel := context.WithTimeout(ctx, w.batchTimeout)
	defer cancel()

	claimCtx, span := tracing.StartSpan(claimCtx, "outbox.worker.process_batch",
		trace.WithAttributes(attribute.Int("batch.max_size", w.batchSize)),
	)
	defer span.End()

	start := time.Now()
	events, err := w.store.ClaimPendingEvents(claimCtx, w.batchSize)
	if err != nil {
		tracing.RecordError(span, err)
		w.logger.Error(claimCtx, "Failed to fetch outbox events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	span.SetAttributes(attribute.Int("batch.fetched", len(events)))

	for _, event := range events {
		w.processEvent(ctx, event)
	}

	metrics.ObserveWithTrace(ctx,
		metrics.OutboxWorkerDuration.WithLabelValues(w.cfg.Service.Name),
		time.Since(start).Seconds(),
	)
}

func (w *Worker) releaseStuck(ctx context.Context) {
	releaseCtx, cancel := w.newOperationContext(ctx)
	defer cancel()

	releaseCtx, span := tracing.StartSpan(releaseCtx, "outbox.worker.release_stuck")
	defer span.End()

	stuckBefore := time.Now().UTC().Add(-w.cfg.Kafka.Outbox.ProcessingTimeout)

	released, err := w.store.ReleaseStuckEvents(releaseCtx, stuckBefore)
	if err != nil {
		tracing.RecordError(span, err)
		w.logger.Error(releaseCtx, "Failed to release stuck outbox events", zap.Error(err))
		return
	}

	if released > 0 {
		span.SetAttributes(attribute.Int64("released_count", released))
		w.logger.Warn(releaseCtx, "Released stuck outbox events back to pending",
			zap.Int64("count", released),
			zap.Time("stuck_before", stuckBefore),
		)
		metrics.OutboxEventsTotal.WithLabelValues(w.cfg.Service.Name, "any", "released_from_stuck").Inc()
	}
}

func (w *Worker) processEvent(ctx context.Context, event models.OutboxEvent) {
	sendCtx, cancel := context.WithTimeout(ctx, w.batchTimeout)
	defer cancel()

	sendCtx, span := tracing.StartSpan(sendCtx, "outbox.worker.publish_event",
		trace.WithAttributes(
			attribute.String("event_type", event.EventType),
			attribute.String("aggregate_id", event.AggregateID.String()),
			attribute.Int("retry_count", event.RetryCount),
		),
	)
	defer span.End()

	if err := w.publisher.Send(sendCtx, w.messageKey(event), event.Payload); err != nil {
		tracing.RecordError(span, err)
		w.handleSendError(ctx, event, err)
		return
	}

	stateCtx, stateCancel := w.newOperationContext(ctx)
	defer stateCancel()

	if err := w.store.MarkPublished(stateCtx, event); err != nil {
		tracing.RecordError(span, err)

		w.logger.Error(stateCtx, "Published to Kafka but failed to mark outbox event as published — possible duplicate",
			zap.String("outbox_id", event.ID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Error(err),
		)
		return
	}

	metrics.OutboxEventsTotal.WithLabelValues(w.cfg.Service.Name, event.EventType, "published").Inc()

	w.logger.Info(stateCtx, "Outbox event published",
		zap.String("event_type", event.EventType),
		zap.String("aggregate_id", event.AggregateID.String()),
		zap.String("event_id", event.EventID.String()),
	)
}

func (w *Worker) handleSendError(ctx context.Context, event models.OutboxEvent, sendErr error) {
	stateCtx, cancel := w.newOperationContext(ctx)
	defer cancel()

	nextRetryCount := event.RetryCount + 1
	errText := sendErr.Error()

	if nextRetryCount >= w.maxRetries {
		w.logger.Error(stateCtx, "Outbox event exhausted retries, marking as failed",
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.Int("retry_count", event.RetryCount),
			zap.Error(sendErr),
		)
		if markErr := w.store.MarkFailed(stateCtx, event, errText); markErr != nil {
			w.logger.Error(stateCtx, "Failed to mark outbox event as failed",
				zap.String("outbox_id", event.ID.String()),
				zap.Error(markErr),
			)
		}
		return
	}

	backoff := time.Duration(nextRetryCount) * w.pollInterval
	nextRetryAt := time.Now().UTC().Add(backoff)

	if retryErr := w.store.ScheduleRetry(stateCtx, event, nextRetryAt, errText); retryErr != nil {
		w.logger.Error(stateCtx, "Failed to schedule retry for outbox event",
			zap.String("outbox_id", event.ID.String()),
			zap.Error(retryErr),
		)
		return
	}

	w.logger.Warn(stateCtx, "Failed to publish outbox event, scheduled retry",
		zap.String("event_type", event.EventType),
		zap.String("aggregate_id", event.AggregateID.String()),
		zap.Int("retry_count", nextRetryCount),
		zap.Time("next_retry_at", nextRetryAt),
		zap.Error(sendErr),
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
