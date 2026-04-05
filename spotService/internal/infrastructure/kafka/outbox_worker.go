package outbox

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
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
	cfg          config.SpotConfig
}

func NewWorker(
	store EventStore,
	publisher EventPublisher,
	pollInterval time.Duration,
	batchSize int,
	timeout time.Duration,
	maxRetries int,
	logger *zapLogger.Logger,
	cfg config.SpotConfig,
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
//   - cleanupTicker (processingTimeout) — сброс stuck processing-событий в pending
func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil {
		return serviceErrors.ErrNilContext
	}

	processingTimeout := w.cfg.Kafka.Outbox.ProcessingTimeout

	w.logger.Info(ctx, "Outbox worker started",
		zap.Duration("poll_interval", w.pollInterval),
		zap.Int("batch_size", w.batchSize),
		zap.Int("max_retries", w.maxRetries),
		zap.Duration("processing_timeout", processingTimeout),
	)

	w.releaseStuck(ctx)
	w.processBatch(ctx)

	pollTicker := time.NewTicker(w.pollInterval)
	defer pollTicker.Stop()

	cleanupTicker := time.NewTicker(processingTimeout)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info(ctx, "Outbox worker stopped")
			return nil
		case <-pollTicker.C:
			w.processBatch(ctx)
		case <-cleanupTicker.C:
			w.releaseStuck(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	claimCtx, cancel := context.WithTimeout(ctx, w.batchTimeout)
	defer cancel()

	claimCtx, span := tracing.StartSpan(claimCtx, "spot.outbox.worker.process_batch",
		trace.WithAttributes(attributes.BatchSizeValue(w.batchSize)),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(claimCtx,
			metrics.OutboxWorkerDuration.WithLabelValues(w.cfg.Service.Name),
			time.Since(start).Seconds(),
		)
	}()

	events, err := w.store.ClaimPendingEvents(claimCtx, w.batchSize)
	if err != nil {
		tracing.RecordError(span, err)
		w.logContextError(claimCtx, "Outbox claim context finished", err,
			zap.Int("batch_size", w.batchSize),
		)
		w.logger.Error(claimCtx, "Failed to fetch outbox events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	span.SetAttributes(attributes.BatchSizeValue(len(events)))

	for _, event := range events {
		if ctxError := ctx.Err(); ctxError != nil {
			tracing.RecordError(span, ctxError)
			w.logContextError(ctx, "Outbox batch processing interrupted", ctxError)
			return
		}

		w.processEvent(ctx, event)
	}
}

func (w *Worker) releaseStuck(ctx context.Context) {
	releaseCtx, cancel := w.newOperationContext(ctx)
	defer cancel()

	releaseCtx, span := tracing.StartSpan(releaseCtx, "spot.outbox.worker.release_stuck")
	defer span.End()

	stuckBefore := time.Now().UTC().Add(-w.cfg.Kafka.Outbox.ProcessingTimeout)

	released, err := w.store.ReleaseStuckEvents(releaseCtx, stuckBefore)
	if err != nil {
		tracing.RecordError(span, err)
		w.logContextError(releaseCtx, "Outbox release-stuck context finished", err,
			zap.Time("stuck_before", stuckBefore),
		)
		w.logger.Error(releaseCtx, "Failed to release stuck outbox events", zap.Error(err))
		return
	}

	if released > 0 {
		span.SetAttributes(attributes.ReleasedEventCountValue(released))
		w.logger.Warn(releaseCtx, "Released stuck outbox events back to pending",
			zap.Int64("count", released),
			zap.Time("stuck_before", stuckBefore),
		)
		metrics.OutboxEventsTotal.WithLabelValues(w.cfg.Service.Name, "any", "released_from_stuck").Add(float64(released))
	}
}

func (w *Worker) processEvent(ctx context.Context, event models.OutboxEvent) {
	sendCtx, cancel := context.WithTimeout(ctx, w.batchTimeout)
	defer cancel()

	sendCtx, span := tracing.StartSpan(sendCtx, "spot.outbox.worker.publish_event",
		trace.WithAttributes(
			attributes.EventTypeValue(event.EventType),
			attributes.AggregateIDValue(event.AggregateID.String()),
			attributes.RetryCountValue(event.RetryCount),
		),
	)
	defer span.End()

	if err := w.publisher.Send(sendCtx, w.messageKey(event), event.Payload); err != nil {
		tracing.RecordError(span, err)
		w.logContextError(sendCtx, "Outbox publishing context finished", err,
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.String("event_id", event.EventID.String()),
		)
		w.handleSendError(sendCtx, event, err)
		return
	}

	stateCtx, stateCancel := w.newOperationContext(sendCtx)
	defer stateCancel()

	if err := w.store.MarkPublished(stateCtx, event); err != nil {
		tracing.RecordError(span, err)
		w.logContextError(stateCtx, "Outbox mark-published context finished", err,
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.String("event_id", event.EventID.String()),
		)

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

	// maxRetries = число повторных попыток после первой отправки
	// event.RetryCount = сколько попыток уже было использовано (число неудачных попыток)
	if event.RetryCount >= w.maxRetries {
		w.logger.Error(stateCtx, "Outbox event exhausted retries, marking as failed",
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.Int("retry_count", event.RetryCount),
			zap.Error(sendErr),
		)
		if markErr := w.store.MarkFailed(stateCtx, event, errText); markErr != nil {
			w.logContextError(stateCtx, "Outbox mark-failed context finished", markErr,
				zap.String("event_type", event.EventType),
				zap.String("aggregate_id", event.AggregateID.String()),
				zap.String("event_id", event.EventID.String()),
			)
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
		w.logContextError(stateCtx, "Outbox schedule-retry context finished", retryErr,
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.String("event_id", event.EventID.String()),
			zap.Time("next_retry_at", nextRetryAt),
		)
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

// События для одного рынка идут в одну партицию
func (w *Worker) messageKey(event models.OutboxEvent) []byte {
	return []byte(event.AggregateID.String())
}

func (w *Worker) logContextError(ctx context.Context, message string, err error, fields ...zap.Field) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		w.logger.Warn(ctx, message,
			append(fields, zap.Error(err), zap.String("reason", "deadline_exceeded"))...,
		)
	case errors.Is(err, context.Canceled):
		w.logger.Info(ctx, message,
			append(fields, zap.Error(err), zap.String("reason", "context_canceled"))...,
		)
	}
}
