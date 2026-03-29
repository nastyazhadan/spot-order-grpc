package spot

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
)

const marketStateChangedPollerName = "market_state_changed_poller"

type CursorStore interface {
	Get(ctx context.Context, pollerName string) (models.PollerCursor, error)
}

type MarketReader interface {
	ListUpdatedSince(ctx context.Context, since time.Time, afterID uuid.UUID, limit int) ([]sharedModels.Market, error)
}

type MarketEventProducer interface {
	PublishMarketStateChanged(ctx context.Context, events []sharedModels.MarketStateChangedEvent, cursor models.PollerCursor) error
}

type MarketCacheRefresher interface {
	RefreshAll(ctx context.Context) error
}

type MarketPoller struct {
	reader            MarketReader
	producer          MarketEventProducer
	cursorStore       CursorStore
	cacheRefresher    MarketCacheRefresher
	pollInterval      time.Duration
	processingTimeout time.Duration
	batchSize         int
	pollerName        string
	lastSeenAt        time.Time
	lastSeenID        uuid.UUID
	logger            *zapLogger.Logger
}

func NewMarketPoller(
	reader MarketReader,
	producer MarketEventProducer,
	store CursorStore,
	cacheRefresher MarketCacheRefresher,
	interval time.Duration,
	timeout time.Duration,
	size int,
	logger *zapLogger.Logger,
) *MarketPoller {
	return &MarketPoller{
		reader:            reader,
		producer:          producer,
		cursorStore:       store,
		cacheRefresher:    cacheRefresher,
		pollInterval:      interval,
		processingTimeout: timeout,
		batchSize:         size,
		pollerName:        marketStateChangedPollerName,
		logger:            logger,
	}
}

func (p *MarketPoller) Run(ctx context.Context) {
	if err := p.loadCursor(ctx); err != nil {
		p.logger.Error(ctx, "Failed to load market poller cursor", zap.Error(err))
		return
	}

	p.poll(ctx)

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	p.logger.Info(ctx, "Market poller started",
		zap.Duration("poll_interval", p.pollInterval),
		zap.Duration("processing_timeout", p.processingTimeout),
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

func (p *MarketPoller) loadCursor(ctx context.Context) error {
	cursor, err := p.cursorStore.Get(ctx, p.pollerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			p.lastSeenAt = time.Time{}
			p.lastSeenID = uuid.Nil
			return nil
		}
		return err
	}

	p.lastSeenAt = cursor.LastSeenAt.UTC()
	p.lastSeenID = cursor.LastSeenID
	return nil
}

func (p *MarketPoller) poll(ctx context.Context) {
	pollCtx, cancel := context.WithTimeout(ctx, p.processingTimeout)
	defer cancel()

	needCacheRefresh := false
	defer func() {
		p.refreshCache(ctx, needCacheRefresh)
	}()

	for {
		if pollCtx.Err() != nil {
			return
		}

		updated, hasMore, err := p.processNextBatch(pollCtx)
		if updated {
			needCacheRefresh = true
		}
		if err != nil {
			return
		}

		if !hasMore {
			return
		}
	}
}

func (p *MarketPoller) processNextBatch(ctx context.Context) (updated, hasMore bool, err error) {
	markets, err := p.reader.ListUpdatedSince(ctx, p.lastSeenAt, p.lastSeenID, p.batchSize)
	if err != nil {
		p.logger.Error(ctx, "Failed to load updated markets", zap.Error(err))
		return false, false, err
	}

	if len(markets) == 0 {
		return false, false, nil
	}

	events, err := p.buildMarketStateChangedEvents(ctx, markets)
	if err != nil {
		return true, false, err
	}

	nextCursor := p.buildNextCursor(markets)

	if err = p.producer.PublishMarketStateChanged(ctx, events, nextCursor); err != nil {
		p.logger.Error(ctx, "Failed to enqueue market state changed batch",
			zap.Int("markets_count", len(markets)),
			zap.Time("last_seen_at", nextCursor.LastSeenAt),
			zap.String("last_seen_id", nextCursor.LastSeenID.String()),
			zap.Error(err),
		)
		return true, false, err
	}

	p.lastSeenAt = nextCursor.LastSeenAt
	p.lastSeenID = nextCursor.LastSeenID

	return true, len(markets) == p.batchSize, nil
}

func (p *MarketPoller) refreshCache(ctx context.Context, needRefresh bool) {
	if !needRefresh || p.cacheRefresher == nil {
		return
	}

	refreshCtx, refreshCancel := context.WithTimeout(context.WithoutCancel(ctx), p.processingTimeout)
	defer refreshCancel()

	if err := p.cacheRefresher.RefreshAll(refreshCtx); err != nil {
		p.logger.Warn(refreshCtx, "Failed to refresh market cache after updates", zap.Error(err))
	}
}

func (p *MarketPoller) buildMarketStateChangedEvents(
	ctx context.Context,
	markets []sharedModels.Market,
) ([]sharedModels.MarketStateChangedEvent, error) {
	events := make([]sharedModels.MarketStateChangedEvent, 0, len(markets))

	for _, market := range markets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		event := sharedModels.MarketStateChangedEvent{
			EventID:       uuid.New(),
			MarketID:      market.ID,
			Enabled:       market.Enabled,
			DeletedAt:     market.DeletedAt,
			CorrelationID: uuid.New(),
			CausationID:   nil,
			UpdatedAt:     market.UpdatedAt.UTC(),
		}

		events = append(events, event)
	}

	return events, nil
}

func (p *MarketPoller) buildNextCursor(markets []sharedModels.Market) models.PollerCursor {
	last := markets[len(markets)-1]

	return models.PollerCursor{
		PollerName: p.pollerName,
		LastSeenAt: last.UpdatedAt.UTC(),
		LastSeenID: last.ID,
	}
}
