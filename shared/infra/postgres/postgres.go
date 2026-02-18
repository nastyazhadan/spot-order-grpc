package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPgxPool(ctx context.Context, dbURI string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dbURI)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pool.Ping: %w", err)
	}
	return pool, nil
}
