package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

type InboxStore struct {
	pool   *pgxpool.Pool
	config config.OrderConfig
}

func New(pool *pgxpool.Pool, cfg config.OrderConfig) *InboxStore {
	return &InboxStore{
		pool:   pool,
		config: cfg,
	}
}

func (s *InboxStore) BeginProcessing(
	ctx context.Context,
	transaction pgx.Tx,
	event models.InboxEvent,
) (bool, models.InboxEventStatus, error) {
	const op = "InboxStore.BeginProcessing"

	ctx, span := tracing.StartSpan(ctx, "inbox.begin_processing",
		trace.WithAttributes(
			attribute.String("event_id", event.EventID.String()),
			attribute.String("topic", event.Topic),
			attribute.String("consumer_group", event.ConsumerGroup),
		),
	)
	defer span.End()

	started, status, err := s.tryStartProcessing(ctx, transaction, event)
	if err == nil {
		return started, status, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		tracing.RecordError(span, err)
		return false, "", fmt.Errorf("%s: %w", op, err)
	}

	currentStatus, err := s.getCurrentStatus(ctx, transaction, event.EventID, event.ConsumerGroup)
	if err != nil {
		tracing.RecordError(span, err)
		return false, "", fmt.Errorf("%s: get current inbox status: %w", op, err)
	}

	return false, currentStatus, nil
}

func (s *InboxStore) tryStartProcessing(
	ctx context.Context,
	transaction pgx.Tx,
	event models.InboxEvent,
) (bool, models.InboxEventStatus, error) {
	const op = "InboxStore.tryStartProcessing"

	start := time.Now()

	var status models.InboxEventStatus
	err := transaction.QueryRow(ctx, `
		INSERT INTO inbox (id, event_id, topic, consumer_group, payload, status)
		VALUES ($1, $2, $3, $4, $5, 'processing')
		ON CONFLICT (event_id, consumer_group) DO UPDATE
		SET topic = EXCLUDED.topic,
		    payload = EXCLUDED.payload,
		    status = 'processing',
		    failed_at = NULL,
		    processed_at = NULL,
		    error_message = NULL
		WHERE inbox.status = 'failed'
		RETURNING status
	`, event.ID, event.EventID, event.Topic, event.ConsumerGroup, event.Payload).Scan(&status)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.begin_processing"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		return false, "", fmt.Errorf("%s: %w", op, err)
	}

	return true, status, nil
}

func (s *InboxStore) getCurrentStatus(
	ctx context.Context,
	transaction pgx.Tx,
	eventID uuid.UUID,
	consumerGroup string,
) (models.InboxEventStatus, error) {
	const op = "InboxStore.getCurrentStatus"

	start := time.Now()

	var status models.InboxEventStatus
	err := transaction.QueryRow(ctx, `
		SELECT status FROM inbox
		WHERE event_id = $1 AND consumer_group = $2
	`, eventID, consumerGroup).Scan(&status)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.get_current_status"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return status, nil
}

func (s *InboxStore) MarkProcessed(
	ctx context.Context,
	transaction pgx.Tx,
	eventID uuid.UUID,
	consumerGroup string,
) error {
	const op = "InboxStore.MarkProcessed"

	ctx, span := tracing.StartSpan(ctx, "inbox.mark_processed",
		trace.WithAttributes(
			attribute.String("event_id", eventID.String()),
			attribute.String("consumer_group", consumerGroup),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := transaction.Exec(ctx, `
		UPDATE inbox
		SET status = 'processed',
	    	processed_at = NOW(),
	    	failed_at = NULL,
	    	error_message = NULL
		WHERE event_id = $1 AND consumer_group = $2 AND status = 'processing'
	`, eventID, consumerGroup)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.mark_processed"),
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

func (s *InboxStore) SaveFailed(
	ctx context.Context,
	event models.InboxEvent,
	errText string,
) error {
	const op = "InboxStore.SaveFailed"

	ctx, span := tracing.StartSpan(ctx, "inbox.save_failed",
		trace.WithAttributes(
			attribute.String("event_id", event.EventID.String()),
			attribute.String("topic", event.Topic),
			attribute.String("consumer_group", event.ConsumerGroup),
		),
	)
	defer span.End()

	start := time.Now()
	// SaveFailed намеренно выполняется вне откатываемой бизнес-транзакции.
	// При ошибке мы сначала откатываем все изменения в рамках текущей транзакции,
	// а затем отдельным запросом сохраняем статус failed, чтобы эта запись
	// не откатилась вместе с транзакцией
	result, err := s.pool.Exec(ctx, `
		INSERT INTO inbox (id, event_id, topic, consumer_group, payload, status, failed_at, error_message)
		VALUES ($1, $2, $3, $4, $5, 'failed', NOW(), $6)
		ON CONFLICT (event_id, consumer_group)
		DO UPDATE SET
			topic = EXCLUDED.topic,
			payload = EXCLUDED.payload,
			status = 'failed',
			failed_at = NOW(),
			processed_at = NULL,
			error_message = EXCLUDED.error_message
		WHERE inbox.status = 'failed' OR inbox.status = 'processing'
	`, event.ID, event.EventID, event.Topic, event.ConsumerGroup, event.Payload, errText)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.save_failed"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%s: expected to affect 1 row, affected %d", op, result.RowsAffected())
	}

	return nil
}
