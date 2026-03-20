package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/postgres"
)

const serviceName = "spotService"

type MarketStore struct {
	pool *pgxpool.Pool
}

func NewMarketStore(pool *pgxpool.Pool) *MarketStore {
	return &MarketStore{
		pool: pool,
	}
}

func (m *MarketStore) ListAll(ctx context.Context) ([]models.Market, error) {
	const op = "postgres.MarketStore.ListAll"

	ctx, span := tracing.StartSpan(ctx, "postgres.list_all_markets")
	defer span.End()

	start := time.Now()
	rows, err := m.pool.Query(ctx, `SELECT id, name, enabled, deleted_at FROM market_store`)
	if err != nil {
		metrics.DBQueryDuration.WithLabelValues(serviceName, "list_all_markets").Observe(time.Since(start).Seconds())
		span.RecordError(err)

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	marketsDTO, err := pgx.CollectRows(rows, pgx.RowToStructByName[dto.Market])
	metrics.DBQueryDuration.WithLabelValues(serviceName, "list_all_markets").Observe(time.Since(start).Seconds())
	if err != nil {
		span.RecordError(err)
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

	ctx, span := tracing.StartSpan(ctx, "postgres.market_by_id")
	defer span.End()

	start := time.Now()
	rows, err := m.pool.Query(ctx,
		`SELECT id, name, enabled, deleted_at FROM market_store WHERE id = $1`, id)
	if err != nil {
		metrics.DBQueryDuration.WithLabelValues(serviceName, "get_market_by_id").Observe(time.Since(start).Seconds())
		span.RecordError(err)

		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	marketDTO, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[dto.Market])
	metrics.DBQueryDuration.WithLabelValues(serviceName, "get_market_by_id").Observe(time.Since(start).Seconds())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Market{}, fmt.Errorf("%s: %w", op, repositoryErrors.ErrMarketNotFound)
		}

		span.RecordError(err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	return marketDTO.ToDomain(), nil
}
