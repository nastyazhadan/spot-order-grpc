package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/repository/postgres/dto"
)

type MarketStore struct {
	pool *pgxpool.Pool
}

func NewMarketStore(pool *pgxpool.Pool) *MarketStore {
	return &MarketStore{
		pool: pool,
	}
}

func (m *MarketStore) ListAll(ctx context.Context) ([]models.Market, error) {
	const op = "repository.MarketStore.ListAll"

	rows, err := m.pool.Query(ctx, `SELECT id, name, enabled, deleted_at FROM market_store`)
	if err != nil {
		return nil, fmt.Errorf("%s: query: %w", op, err)
	}

	marketsDTO, err := pgx.CollectRows(rows, pgx.RowToStructByName[dto.Market])
	if err != nil {
		return nil, fmt.Errorf("%s: collect: %w", op, err)
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
