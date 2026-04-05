package db

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db/migrator"
)

type PoolConfig struct {
	MaxConnections  int32
	MinConnections  int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func BootstrapPostgres(
	ctx context.Context,
	dbURI string,
	migrationsFS fs.FS,
	config PoolConfig,
) (*pgxpool.Pool, error) {
	const op = "db.BootstrapPostgres"

	pool, err := newPostgresPool(ctx, dbURI, config)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = runPostgresMigrations(ctx, pool, migrationsFS); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return pool, nil
}

func newPostgresPool(
	ctx context.Context,
	dbURI string,
	config PoolConfig,
) (*pgxpool.Pool, error) {
	const op = "db.newPostgresPool"

	cfg, err := pgxpool.ParseConfig(dbURI)
	if err != nil {
		return nil, fmt.Errorf("%s: parse config: %w", op, err)
	}

	applyPoolConfig(cfg, config)

	// При нормальном завершении программы закрывается в lifecycle.go, при ошибке - в postgres.go
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: create pool: %w", op, err)
	}

	ok := false
	defer func() {
		if !ok {
			pool.Close()
		}
	}()

	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("%s: ping db: %w", op, err)
	}

	ok = true
	return pool, nil
}

func applyPoolConfig(
	cfg *pgxpool.Config,
	poolConfig PoolConfig,
) {
	cfg.MaxConns = poolConfig.MaxConnections
	cfg.MinConns = poolConfig.MinConnections
	cfg.MaxConnLifetime = poolConfig.MaxConnLifetime
	cfg.MaxConnIdleTime = poolConfig.MaxConnIdleTime
}

func runPostgresMigrations(
	ctx context.Context,
	pool *pgxpool.Pool,
	migrationsFS fs.FS,
) error {
	const op = "db.runPostgresMigrations"

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	dbMigrator := migrator.NewMigrator(sqlDB, migrationsFS)

	if err := dbMigrator.Up(ctx); err != nil {
		return fmt.Errorf("%s: run migrations: %w", op, err)
	}

	return nil
}
