package producer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
	mapper "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/kafka"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
)

type CursorStore interface {
	SaveCursorTransaction(ctx context.Context, transaction pgx.Tx, cursor models.PollerCursor) error
}

type OutboxWriter interface {
	BeginTransaction(ctx context.Context) (pgx.Tx, error)
	SaveOutboxEvent(ctx context.Context, transaction pgx.Tx, event models.OutboxEvent) error
}

type MarketProducer struct {
	outboxWriter OutboxWriter
	cursorStore  CursorStore
	logger       *zapLogger.Logger
	config       config.SpotConfig
}

func New(writer OutboxWriter, store CursorStore, logger *zapLogger.Logger, cfg config.SpotConfig) *MarketProducer {
	return &MarketProducer{
		outboxWriter: writer,
		cursorStore:  store,
		logger:       logger,
		config:       cfg,
	}
}

// PublishMarketStateChanged не публикует напрямую в Kafka - это делает outbox.Worker асинхронно.
func (p *MarketProducer) PublishMarketStateChanged(
	ctx context.Context,
	events []sharedModels.MarketStateChangedEvent,
	cursor models.PollerCursor,
) error {
	const op = "MarketProducer.PublishMarketStateChanged"

	if len(events) == 0 {
		return nil
	}

	ctx, span := tracing.StartSpan(ctx, "producer.publish_market_state_changed")
	defer span.End()

	transaction, err := p.outboxWriter.BeginTransaction(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: begin tx: %w", op, err)
	}

	committed := false
	defer func() {
		if !committed {
			p.rollbackTransaction(ctx, transaction, p.logger, op)
		}
	}()

	for _, event := range events {
		outboxEvent, buildError := p.buildOutboxEvent(ctx, span, event)
		if buildError != nil {
			tracing.RecordError(span, buildError)
			return fmt.Errorf("%s: build outbox event: %w", op, buildError)
		}

		if err = p.outboxWriter.SaveOutboxEvent(ctx, transaction, outboxEvent); err != nil {
			tracing.RecordError(span, err)
			return fmt.Errorf("%s: save outbox event: %w", op, err)
		}
	}

	if err = p.cursorStore.SaveCursorTransaction(ctx, transaction, cursor); err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: save cursor: %w", op, err)
	}

	if err = p.commitTransaction(ctx, transaction); err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: commit: %w", op, err)
	}

	committed = true
	p.logger.Info(ctx, "MarketStateChangedEvent saved to outbox with cursor",
		zap.Int("events_count", len(events)),
		zap.Time("last_seen_at", cursor.LastSeenAt),
		zap.String("last_seen_id", cursor.LastSeenID.String()),
		zap.String("poller_name", cursor.PollerName),
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

func (p *MarketProducer) rollbackTransaction(
	ctx context.Context,
	transaction pgx.Tx,
	logger *zapLogger.Logger,
	message string,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.Timeouts.Service)
	defer cancel()

	if err := transaction.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		logger.Error(cleanupCtx, message, zap.Error(err))
	}
}

func (p *MarketProducer) commitTransaction(
	ctx context.Context,
	transaction pgx.Tx,
) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.Timeouts.Service)
	defer cancel()

	return transaction.Commit(commitCtx)
}
