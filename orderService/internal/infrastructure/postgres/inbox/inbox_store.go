package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

type InboxStore struct {
	pool   *pgxpool.Pool
	logger *zapLogger.Logger
	config config.OrderConfig
}

func New(pool *pgxpool.Pool, logger *zapLogger.Logger, cfg config.OrderConfig) *InboxStore {
	return &InboxStore{
		pool:   pool,
		logger: logger,
		config: cfg,
	}
}

func (s *InboxStore) BeginProcessing(
	ctx context.Context,
	transaction pgx.Tx,
	event models.InboxEvent,
) (bool, error) {
	const op = "InboxStore.BeginProcessing"

	ctx, span := tracing.StartSpan(ctx, "inbox.begin_processing",
		trace.WithAttributes(
			attribute.String("event_id", event.EventID.String()),
			attribute.String("topic", event.Topic),
			attribute.String("consumer_group", event.ConsumerGroup),
		),
	)
	defer span.End()

	start := time.Now()
	command, err := transaction.Exec(ctx, `
		INSERT INTO inbox (
			id,
			event_id,
			topic,
			consumer_group,
			payload,
			status
		)
		VALUES ($1, $2, $3, $4, $5, 'processing')
		ON CONFLICT (event_id, consumer_group) DO NOTHING
	`, event.ID, event.EventID, event.Topic, event.ConsumerGroup, event.Payload)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.begin_processing"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return false, fmt.Errorf("%s: %w", op, err)
	}

	if command.RowsAffected() == 0 {
		return false, nil
	}

	return true, nil
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
	_, err := transaction.Exec(ctx, `
		UPDATE inbox
		SET status = 'processed',
		    processed_at = NOW(),
		    error_message = NULL
		WHERE event_id = $1
		  AND consumer_group = $2
	`, eventID, consumerGroup)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.mark_processed"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
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
	_, err := s.pool.Exec(ctx, `
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
	`, event.ID, event.EventID, event.Topic, event.ConsumerGroup, event.Payload, errText)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(s.config.Service.Name, "inbox.save_failed"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
