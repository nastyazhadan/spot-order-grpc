package spot

import (
	"context"
	"errors"
	"fmt"
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
	InvalidateByIDs(ctx context.Context, ids []uuid.UUID) error
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

func (p *MarketPoller) Init(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("market poller init: nil context")
	}
	initCtx, cancel := context.WithTimeout(ctx, p.processingTimeout)
	defer cancel()

	if err := p.loadCursor(initCtx); err != nil {
		return fmt.Errorf("load market poller cursor: %w", err)
	}

	p.logger.Info(ctx, "Market poller cursor loaded",
		zap.Time("last_seen_at", p.lastSeenAt),
		zap.String("last_seen_id", p.lastSeenID.String()),
	)

	return nil
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

func (p *MarketPoller) Run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("market poller run: nil context")
	}
	if err := p.poll(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("initial poll: %w", err)
	}

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
			return nil
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("scheduled poll: %w", err)
			}
		}
	}
}

func (p *MarketPoller) poll(ctx context.Context) error {
	pollCtx, cancel := context.WithTimeout(ctx, p.processingTimeout)
	defer cancel()

	var updatedIDs []uuid.UUID

	defer func() {
		p.refreshCache(ctx, updatedIDs)
	}()

	for {
		if err := pollCtx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				p.logger.Warn(ctx, "Market poll timed out before completion",
					zap.Duration("processing_timeout", p.processingTimeout),
					zap.Int("updated_ids_count", len(updatedIDs)),
					zap.Time("last_seen_at", p.lastSeenAt),
					zap.String("last_seen_id", p.lastSeenID.String()),
				)
			}
			return err
		}

		batchUpdatedIDs, hasMore, err := p.processNextBatch(pollCtx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				p.logger.Warn(ctx, "Market poll batch processing timed out",
					zap.Duration("processing_timeout", p.processingTimeout),
					zap.Int("updated_ids_count", len(updatedIDs)),
					zap.Time("last_seen_at", p.lastSeenAt),
					zap.String("last_seen_id", p.lastSeenID.String()),
				)
			}
			return err
		}
		if len(batchUpdatedIDs) > 0 {
			updatedIDs = append(updatedIDs, batchUpdatedIDs...)
		}
		if !hasMore {
			return nil
		}
	}
}

func (p *MarketPoller) refreshCache(
	ctx context.Context,
	updatedIDs []uuid.UUID,
) {
	if len(updatedIDs) == 0 || p.cacheRefresher == nil {
		return
	}

	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.processingTimeout)
	defer cancel()

	if err := p.cacheRefresher.InvalidateByIDs(refreshCtx, updatedIDs); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			p.logger.Warn(refreshCtx, "Failed to invalidate markets by id cache after updates", zap.Error(err))
		}
	}

	if err := p.cacheRefresher.RefreshAll(refreshCtx); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			p.logger.Warn(refreshCtx, "Failed to refresh market cache after updates", zap.Error(err))
		}
	}
}

func (p *MarketPoller) processNextBatch(
	ctx context.Context,
) (updatedIDs []uuid.UUID, hasMore bool, err error) {
	markets, err := p.reader.ListUpdatedSince(ctx, p.lastSeenAt, p.lastSeenID, p.batchSize)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			p.logger.Error(ctx, "Failed to load updated markets", zap.Error(err))
		}
		return nil, false, err
	}

	if len(markets) == 0 {
		return nil, false, nil
	}

	updatedIDs = make([]uuid.UUID, 0, len(markets))
	for _, market := range markets {
		updatedIDs = append(updatedIDs, market.ID)
	}

	events, err := p.buildMarketStateChangedEvents(ctx, markets)
	if err != nil {
		return nil, false, err
	}

	nextCursor := p.buildNextPollerCursor(markets)

	if err = p.producer.PublishMarketStateChanged(ctx, events, nextCursor); err != nil {
		p.logger.Error(ctx, "Failed to enqueue market state changed batch",
			zap.Int("markets_count", len(markets)),
			zap.Time("last_seen_at", nextCursor.LastSeenAt),
			zap.String("last_seen_id", nextCursor.LastSeenID.String()),
			zap.Error(err),
		)
		return nil, false, err
	}

	p.lastSeenAt = nextCursor.LastSeenAt
	p.lastSeenID = nextCursor.LastSeenID

	return updatedIDs, len(markets) == p.batchSize, nil
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
			EventID:   uuid.New(),
			MarketID:  market.ID,
			Enabled:   market.Enabled,
			DeletedAt: market.DeletedAt,
			UpdatedAt: market.UpdatedAt.UTC(),
		}

		events = append(events, event)
	}

	return events, nil
}

func (p *MarketPoller) buildNextPollerCursor(markets []sharedModels.Market) models.PollerCursor {
	last := markets[len(markets)-1]

	return models.PollerCursor{
		PollerName: p.pollerName,
		LastSeenAt: last.UpdatedAt.UTC(),
		LastSeenID: last.ID,
	}
}
