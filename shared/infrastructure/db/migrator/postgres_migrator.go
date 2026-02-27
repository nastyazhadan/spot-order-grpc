package migrator

import (
	"context"
	"database/sql"
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
	goose.SetBaseFS(m.migrationsFS)
	defer goose.SetBaseFS(nil)

	return goose.UpContext(ctx, m.db, ".")
}
