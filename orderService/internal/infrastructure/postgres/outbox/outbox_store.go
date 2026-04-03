package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

// OutboxStore используется воркером Transactional Outbox для публикации событий
type OutboxStore struct {
	pool   *pgxpool.Pool
	logger *zapLogger.Logger
	config config.OrderConfig
}

func New(pool *pgxpool.Pool, logger *zapLogger.Logger, cfg config.OrderConfig) *OutboxStore {
	return &OutboxStore{
		pool:   pool,
		logger: logger,
		config: cfg,
	}
}

// SaveOutboxEvent атомарно сохраняет событие в таблицу outbox в рамках переданной транзакции.
// Должна вызываться внутри той же транзакции, что и SaveOrder
func (s *OutboxStore) SaveOutboxEvent(ctx context.Context, transaction pgx.Tx, event models.OutboxEvent) error {
	const op = "OutboxStore.SaveOutboxEvent"

	ctx, span := tracing.StartSpan(ctx, "outbox.save_event",
		trace.WithAttributes(
			attributes.EventIDValue(event.EventID.String()),
			attributes.EventTypeValue(event.EventType),
			attributes.AggregateIDValue(event.AggregateID.String()),
		),
	)
	defer span.End()

	start := time.Now()
	_, err := transaction.Exec(ctx, `
		INSERT INTO outbox (id, event_id, event_type, aggregate_id, payload, status, retry_count, available_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', 0, NOW())
	`, event.ID, event.EventID, event.EventType, event.AggregateID, event.Payload)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "outbox.save"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		s.logger.Error(ctx, "Failed to save outbox event",
			zap.String("event_id", event.EventID.String()),
			zap.String("event_type", event.EventType),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// ClaimPendingEvents выбирает пакет событий со статусом pending и блокирует строки (SELECT FOR UPDATE SKIP LOCKED).
// SKIP LOCKED позволяет нескольким воркерам работать параллельно без конфликтов
func (s *OutboxStore) ClaimPendingEvents(ctx context.Context, limit int) ([]models.OutboxEvent, error) {
	const op = "OutboxStore.ClaimPendingEvents"

	ctx, span := tracing.StartSpan(ctx, "outbox.claim_pending",
		trace.WithAttributes(attributes.BatchLimitValue(limit)),
	)
	defer span.End()

	start := time.Now()
	rows, err := s.pool.Query(ctx, `
		WITH candidates AS (
			SELECT id FROM outbox
			WHERE status = 'pending' AND available_at <= NOW()
			ORDER BY created_at, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox o
		SET status = 'processing', locked_at = NOW() 
		FROM candidates c
		WHERE o.id = c.id
		RETURNING
			o.id, o.event_id, o.event_type, o.aggregate_id, o.payload, o.status, o.retry_count,
			o.available_at, o.created_at, o.published_at, o.failed_at, o.locked_at, o.last_error
	`, limit)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "outbox.claim_pending"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: query: %w", op, err)
	}
	defer rows.Close()

	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.OutboxEvent])
	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: collect: %w", op, err)
	}

	span.SetAttributes(attributes.BatchSizeValue(len(events)))

	if len(events) == 0 {
		return events, nil
	}

	return events, nil
}

func (s *OutboxStore) MarkPublished(ctx context.Context, event models.OutboxEvent) error {
	const op = "OutboxStore.MarkPublished"

	ctx, span := tracing.StartSpan(ctx, "outbox.mark_published",
		trace.WithAttributes(
			attributes.OutboxIDValue(event.ID.String()),
			attributes.EventIDValue(event.EventID.String()),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := s.pool.Exec(ctx, `
		UPDATE outbox
		SET status = 'published',
	    	published_at = NOW(),
	    	failed_at = NULL,
	    	locked_at = NULL,
	    	last_error = NULL
		WHERE id = $1 AND status = 'processing'
	`, event.ID)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "outbox.mark_published"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%s: expected to update 1 row, updated %d", op, result.RowsAffected())
	}

	return nil
}

func (s *OutboxStore) ScheduleRetry(
	ctx context.Context,
	event models.OutboxEvent,
	nextRetryAt time.Time,
	errText string,
) error {
	const op = "OutboxStore.ScheduleRetry"

	ctx, span := tracing.StartSpan(ctx, "outbox.schedule_retry",
		trace.WithAttributes(
			attributes.OutboxIDValue(event.ID.String()),
			attributes.EventIDValue(event.EventID.String()),
			attributes.RetryCountValue(event.RetryCount),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := s.pool.Exec(ctx, `
		UPDATE outbox
		SET status = 'pending',
	    	retry_count = retry_count + 1,
	    	available_at = $2,
	    	failed_at = NULL,
	    	locked_at = NULL,
	    	last_error = $3
		WHERE id = $1 AND status = 'processing'
	`, event.ID, nextRetryAt.UTC(), errText)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "outbox.schedule_retry"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%s: expected to update 1 row, updated %d", op, result.RowsAffected())
	}

	return nil
}

func (s *OutboxStore) MarkFailed(ctx context.Context, event models.OutboxEvent, errText string) error {
	const op = "OutboxStore.MarkFailed"

	ctx, span := tracing.StartSpan(ctx, "outbox.mark_failed",
		trace.WithAttributes(
			attributes.OutboxIDValue(event.ID.String()),
			attributes.EventIDValue(event.EventID.String()),
			attributes.RetryCountValue(event.RetryCount),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := s.pool.Exec(ctx, `
		UPDATE outbox
		SET status = 'failed',
	    	retry_count = retry_count + 1,
	    	failed_at = NOW(),
	    	locked_at = NULL,
	    	last_error = $2
		WHERE id = $1 AND status = 'processing'
	`, event.ID, errText)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "outbox.mark_failed"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%s: expected to update 1 row, updated %d", op, result.RowsAffected())
	}

	metrics.OutboxEventsTotal.WithLabelValues(s.config.Service.Name, event.EventType, "failed").Inc()

	return nil
}

// ReleaseStuckEvents сбрасывает застрявшие события обратно в статус pending.
// Застрявшее событие — то, которое осталось в processing дольше processingTimeout.
func (s *OutboxStore) ReleaseStuckEvents(ctx context.Context, stuckBefore time.Time) (int64, error) {
	const op = "OutboxStore.ReleaseStuckEvents"

	ctx, span := tracing.StartSpan(ctx, "outbox.release_stuck",
		trace.WithAttributes(
			attributes.StuckEventValue(stuckBefore.String()),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := s.pool.Exec(ctx, `
		UPDATE outbox
		SET status    = 'pending',
		    locked_at = NULL
		WHERE status = 'processing'
		  AND locked_at < $1
	`, stuckBefore)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "outbox.release_stuck"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	released := result.RowsAffected()
	span.SetAttributes(attributes.ReleasedEventCountValue(released))

	return released, nil
}

func (s *OutboxStore) countPendingEvents(ctx context.Context) (int64, error) {
	var pendingCount int64

	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM outbox
		WHERE status = 'pending' AND available_at <= NOW()
	`).Scan(&pendingCount)
	if err != nil {
		return 0, fmt.Errorf("count pending outbox events: %w", err)
	}

	return pendingCount, nil
}
