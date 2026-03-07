package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/spf13/viper"
)

func Load() (*config.SpotConfig, error) {
	if err := config.LoadAll(); err != nil {
		return nil, err
	}

	var cfg config.SpotConfig
	if err := viper.UnmarshalKey("spot", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal spot config: %w", err)
	}

	cfg.DBURI = os.Getenv("SPOT_DB_URI")
	if cfg.DBURI == "" {
		return nil, errors.New("SPOT_DB_URI is required")
	}

	return &cfg, nil
}
