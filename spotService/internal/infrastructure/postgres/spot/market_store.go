package spot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/postgres"
)

const (
	roleAdminKey  = "admin"
	roleViewerKey = "viewer"
)

type MarketStore struct {
	pool   *pgxpool.Pool
	config config.SpotConfig
}

func NewMarketStore(pool *pgxpool.Pool, cfg config.SpotConfig) *MarketStore {
	return &MarketStore{
		pool:   pool,
		config: cfg,
	}
}

func (m *MarketStore) GetMarketsPage(
	ctx context.Context,
	roleKey string,
	limit, offset uint64,
) ([]models.Market, error) {
	const op = "postgres.MarketStore.GetMarketsPage"

	ctx, span := tracing.StartSpan(ctx, "postgres.get_markets_page",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.DBQueryDuration.WithLabelValues(m.config.Service.Name, "get_markets_page"),
			time.Since(start).Seconds(),
		)
	}()

	dtoMarkets, loadError := m.loadMarketsPage(ctx, roleKey, limit, offset)
	if loadError != nil {
		tracing.RecordError(span, loadError)
		return nil, fmt.Errorf("%s: %w", op, loadError)
	}

	if len(dtoMarkets) == 0 {
		markets, err := m.emptyPageResult(ctx, offset)
		if err != nil {
			tracing.RecordError(span, err)
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		return markets, nil
	}

	return dtoMarketsToDomain(dtoMarkets), nil
}

func (m *MarketStore) loadMarketsPage(
	ctx context.Context,
	roleKey string,
	limit, offset uint64,
) ([]dto.Market, error) {
	const op = "postgres.MarketStore.loadMarketsPage"

	whereClause := "deleted_at IS NULL AND enabled = TRUE"
	switch roleKey {
	case roleAdminKey:
		whereClause = "TRUE"
	case roleViewerKey:
		whereClause = "deleted_at IS NULL"
	}

	query := fmt.Sprintf(`
		SELECT id, name, enabled, deleted_at, updated_at 
		FROM market_store
		WHERE %s
		ORDER BY name, id
		LIMIT $1 OFFSET $2
	`, whereClause)

	rows, err := m.pool.Query(ctx, query, int(limit+1), int(offset))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	marketsDTO, err := pgx.CollectRows(rows, pgx.RowToStructByName[dto.Market])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return marketsDTO, nil
}

func (m *MarketStore) emptyPageResult(
	ctx context.Context,
	offset uint64,
) ([]models.Market, error) {
	if offset > 0 {
		return []models.Market{}, nil
	}

	exists, err := m.hasAnyMarkets(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, repositoryErrors.ErrMarketStoreIsEmpty
	}

	return []models.Market{}, nil
}

func (m *MarketStore) hasAnyMarkets(
	ctx context.Context,
) (bool, error) {
	var exists bool

	err := m.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM market_store
		)
	`).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func (m *MarketStore) GetMarketByID(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	const op = "postgres.MarketStore.GetMarketById"

	ctx, span := tracing.StartSpan(ctx, "postgres.get_market_by_id",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.DBQueryDuration.WithLabelValues(m.config.Service.Name, "get_market_by_id"),
			time.Since(start).Seconds(),
		)
	}()

	rows, err := m.pool.Query(ctx, `
		SELECT id, name, enabled, deleted_at, updated_at FROM market_store 
		WHERE id = $1
	`, id)
	if err != nil {
		tracing.RecordError(span, err)

		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	marketDTO, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[dto.Market])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Market{}, fmt.Errorf("%s: %w", op, repositoryErrors.ErrMarketNotFound)
		}

		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	return marketDTO.ToDomain(), nil
}

func (m *MarketStore) ListUpdatedSince(
	ctx context.Context,
	updatedAt time.Time,
	lastID uuid.UUID,
	limit int,
) ([]models.Market, error) {
	const op = "postgres.MarketStore.ListUpdatedSince"

	ctx, span := tracing.StartSpan(ctx, "postgres.list_updated_since",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.DBQueryDuration.WithLabelValues(m.config.Service.Name, "market.list_updated_since"),
			time.Since(start).Seconds(),
		)
	}()

	rows, err := m.pool.Query(ctx, `
		SELECT id, name, enabled, deleted_at, updated_at 
		FROM market_store
		WHERE (updated_at, id) > ($1, $2)
		ORDER BY updated_at, id
		LIMIT $3
	`, updatedAt.UTC(), lastID, limit)
	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: query updated markets: %w", op, err)
	}
	defer rows.Close()

	dtoMarkets, err := pgx.CollectRows(rows, pgx.RowToStructByName[dto.Market])
	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: collect rows: %w", op, err)
	}

	return dtoMarketsToDomain(dtoMarkets), nil
}

func dtoMarketsToDomain(dtoMarkets []dto.Market) []models.Market {
	markets := make([]models.Market, 0, len(dtoMarkets))
	for _, dtoMarket := range dtoMarkets {
		markets = append(markets, dtoMarket.ToDomain())
	}
	return markets
}
