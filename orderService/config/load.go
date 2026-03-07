package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/spf13/viper"
)

func Load() (*config.OrderConfig, error) {
	if err := config.LoadAll(); err != nil {
		return nil, err
	}

	var cfg config.OrderConfig
	if err := viper.UnmarshalKey("order", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal order config: %w", err)
	}

	cfg.DBURI = os.Getenv("ORDER_DB_URI")
	if cfg.DBURI == "" {
		return nil, errors.New("ORDER_DB_URI is required")
	}

	return &cfg, nil
}
