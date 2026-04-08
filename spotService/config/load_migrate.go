package config

import (
	"errors"
	"os"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

type MigrateConfig struct {
	DBURI        string
	PostgresPool config.PostgresPoolConfig
}

type migrateSection struct {
	PostgresPool config.PostgresPoolConfig `mapstructure:"postgres_pool"`
}

func LoadMigrate() (*MigrateConfig, error) {
	var section migrateSection
	if err := config.LoadKey(configDir, "spot", &section); err != nil {
		return nil, err
	}

	dbURI := os.Getenv("SPOT_DB_URI")
	if dbURI == "" {
		return nil, errors.New("SPOT_DB_URI is required")
	}

	cfg := &MigrateConfig{
		DBURI:        dbURI,
		PostgresPool: section.PostgresPool,
	}

	if err := validateMigrateConfig(*cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func validateMigrateConfig(cfg MigrateConfig) error {
	if cfg.DBURI == "" {
		return errors.New("SPOT_DB_URI is required")
	}

	if err := config.ValidatePostgresPoolConfig("postgres_pool", cfg.PostgresPool); err != nil {
		return err
	}

	return nil
}
