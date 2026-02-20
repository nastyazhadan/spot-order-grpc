package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nastyazhadan/spot-order-grpc/shared/infra/db/migrator"
)

func SetupDB(ctx context.Context, dbURI string, migrationsFS fs.FS) (*pgxpool.Pool, error) {
	pool, err := newPgxPool(ctx, dbURI)
	if err != nil {
		return nil, fmt.Errorf("NewPgxPool: %w", err)
	}

	sqlDB, err := sql.Open("pgx", dbURI)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	defer sqlDB.Close()

	dbMigrator := migrator.NewMigrator(sqlDB, migrationsFS)
	if err := dbMigrator.Up(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrator.Up: %w", err)
	}

	return pool, nil
}

func newPgxPool(ctx context.Context, dbURI string) (*pgxpool.Pool, error) {
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
