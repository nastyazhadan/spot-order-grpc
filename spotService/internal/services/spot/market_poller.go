package spot

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketReader interface {
	ListUpdatedSince(ctx context.Context, since time.Time, afterID uuid.UUID, limit int) ([]models.Market, error)
}

type MarketEventProducer interface {
	ProduceMarketStateChanged(ctx context.Context, event models.MarketStateChangedEvent) error
}

type MarketPoller struct {
	reader       MarketReader
	producer     MarketEventProducer
	pollInterval time.Duration
	batchSize    int
	lastSeenAt   time.Time
	lastSeenID   uuid.UUID
	logger       *zapLogger.Logger
}

func NewMarketPoller(
	reader MarketReader,
	producer MarketEventProducer,
	interval time.Duration,
	size int,
	logger *zapLogger.Logger,
) *MarketPoller {
	return &MarketPoller{
		reader:       reader,
		producer:     producer,
		pollInterval: interval,
		batchSize:    size,
		logger:       logger,
	}
}

func (p *MarketPoller) Run(ctx context.Context) {
	if p.lastSeenAt.IsZero() {
		p.lastSeenAt = time.Now().UTC()
		p.lastSeenID = uuid.Nil
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	p.logger.Info(ctx, "Market poller started",
		zap.Duration("poll_interval", p.pollInterval),
		zap.Int("batch_size", p.batchSize),
		zap.Time("last_seen_at", p.lastSeenAt),
		zap.String("last_seen_id", p.lastSeenID.String()),
	)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info(ctx, "Market poller stopped")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *MarketPoller) poll(ctx context.Context) {
	pollCtx, cancel := context.WithTimeout(ctx, p.pollInterval)
	defer cancel()

	for {
		if pollCtx.Err() != nil {
			return
		}

		markets, err := p.reader.ListUpdatedSince(pollCtx, p.lastSeenAt, p.lastSeenID, p.batchSize)
		if err != nil {
			p.logger.Error(pollCtx, "Failed to load updated markets", zap.Error(err))
			return
		}

		if len(markets) == 0 {
			return
		}

		for _, market := range markets {
			if pollCtx.Err() != nil {
				return
			}

			event := p.buildMarketStateChangedEvent(market)
			if err = p.producer.ProduceMarketStateChanged(pollCtx, event); err != nil {
				p.logger.Error(pollCtx, "Failed to enqueue market state changed event",
					zap.String("market_id", market.ID.String()),
					zap.Error(err),
				)
				return
			}
		}

		last := markets[len(markets)-1]
		p.lastSeenAt = last.UpdatedAt.UTC()
		p.lastSeenID = last.ID

		if len(markets) < p.batchSize {
			return
		}
	}
}

func (p *MarketPoller) buildMarketStateChangedEvent(market models.Market) models.MarketStateChangedEvent {
	return models.MarketStateChangedEvent{
		EventID:       uuid.New(),
		MarketID:      market.ID,
		Enabled:       market.Enabled,
		DeletedAt:     market.DeletedAt,
		CorrelationID: uuid.New(),
		CausationID:   nil,
		UpdatedAt:     market.UpdatedAt.UTC(),
	}
}
