package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

type Migrator struct {
	db           *sql.DB
	migrationsFS fs.FS
}

func NewMigrator(db *sql.DB, migrationsFS fs.FS) *Migrator {
	return &Migrator{
		db:           db,
		migrationsFS: migrationsFS,
	}
}

func (m *Migrator) Up(ctx context.Context) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, m.db, m.migrationsFS)
	if err != nil {
		return fmt.Errorf("goose.NewProvider: %w", err)
	}
	defer provider.Close()

	_, err = provider.Up(ctx)
	return err
}
