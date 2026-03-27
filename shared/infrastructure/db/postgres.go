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

func SetupDBWithPoolConfig(
	ctx context.Context,
	dbURI string,
	migrationsFS fs.FS,
	config PoolConfig,
) (*pgxpool.Pool, error) {
	pool, err := newPgxPool(ctx, dbURI, config)
	if err != nil {
		return nil, fmt.Errorf("NewPgxPool: %w", err)
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	dbMigrator := migrator.NewMigrator(sqlDB, migrationsFS)
	if err = dbMigrator.Up(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrator.Up: %w", err)
	}

	return pool, nil
}

func newPgxPool(
	ctx context.Context,
	dbURI string,
	config PoolConfig,
) (*pgxpool.Pool, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	cfg, err := pgxpool.ParseConfig(dbURI)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig: %w", err)
	}

	if config.MaxConnections > 0 {
		cfg.MaxConns = config.MaxConnections
	}
	if config.MinConnections > 0 {
		cfg.MinConns = config.MinConnections
	}
	if config.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = config.MaxConnLifetime
	}
	if config.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = config.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.NewWithConfig: %w", err)
	}

	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pool.Ping: %w", err)
	}

	return pool, nil
}

func (c PoolConfig) validate() error {
	if c.MaxConnections > 0 && c.MinConnections > c.MaxConnections {
		return fmt.Errorf("invalid pool config: MinConns (%d) > MaxConns (%d)",
			c.MinConnections, c.MaxConnections)
	}

	if c.MaxConnections == 0 && c.MinConnections > 0 {
		return fmt.Errorf("invalid pool config: MinConns (%d) set but MaxConns is not",
			c.MinConnections)
	}

	return nil
}
