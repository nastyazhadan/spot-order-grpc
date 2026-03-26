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

func (m *MarketStore) ListAll(ctx context.Context) ([]models.Market, error) {
	const op = "postgres.MarketStore.ListAll"

	ctx, span := tracing.StartSpan(ctx, "postgres.list_all_markets",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.DBQueryDuration.WithLabelValues(m.config.Service.Name, "list_all_markets"),
			time.Since(start).Seconds(),
		)
	}()

	rows, err := m.pool.Query(ctx, `
		SELECT id, name, enabled, deleted_at, updated_at FROM market_store
	`)
	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	marketsDTO, err := pgx.CollectRows(rows, pgx.RowToStructByName[dto.Market])
	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if len(marketsDTO) == 0 {
		return nil, fmt.Errorf("%s: %w", op, repositoryErrors.ErrMarketStoreIsEmpty)
	}

	out := make([]models.Market, 0, len(marketsDTO))
	for _, marketDTO := range marketsDTO {
		out = append(out, marketDTO.ToDomain())
	}

	return out, nil
}

func (m *MarketStore) GetByID(ctx context.Context, id uuid.UUID) (models.Market, error) {
	const op = "postgres.MarketStore.GetById"

	ctx, span := tracing.StartSpan(ctx, "postgres.market_by_id",
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
	since time.Time,
	afterID uuid.UUID,
	limit int,
) ([]models.Market, error) {
	const op = "MarketStore.ListUpdatedSince"

	start := time.Now()
	rows, err := m.pool.Query(ctx, `
		SELECT id, name, enabled, deleted_at, updated_at FROM market_store
		WHERE (updated_at > $1) OR (updated_at = $1 AND id > $2)
		ORDER BY updated_at ASC, id ASC
		LIMIT $3
	`, since.UTC(), afterID, limit)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(m.config.Service.Name, "market.list_updated_since"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		return nil, fmt.Errorf("%s: query: %w", op, err)
	}
	defer rows.Close()

	dtoMarkets, err := pgx.CollectRows(rows, pgx.RowToStructByName[dto.Market])
	if err != nil {
		return nil, fmt.Errorf("%s: collect: %w", op, err)
	}

	markets := make([]models.Market, 0, len(dtoMarkets))
	for _, dtoMarket := range dtoMarkets {
		markets = append(markets, dtoMarket.ToDomain())
	}

	return markets, nil
}
