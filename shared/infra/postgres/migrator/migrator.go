package migrator

import (
	"database/sql"

	"github.com/pressly/goose/v3"
)

type Migrator struct {
	db            *sql.DB
	migrationsDir string
}

func NewMigrator(db *sql.DB, migrationsDir string) *Migrator {
	return &Migrator{
		db:            db,
		migrationsDir: migrationsDir,
	}
}

func (migrator *Migrator) Up() error {
	err := goose.Up(migrator.db, migrator.migrationsDir)
	if err != nil {
		return err
	}

	return nil
}
