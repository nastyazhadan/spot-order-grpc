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

	if err := validateSpotTimeouts(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateSpotTimeouts(cfg config.SpotConfig) error {
	if cfg.Redis.ConnectionTimeout >= cfg.LoadMarketsTimeout {
		return fmt.Errorf(
			"redis.connection_timeout (%s) must be less than load_markets_timeout (%s)",
			cfg.Redis.ConnectionTimeout, cfg.LoadMarketsTimeout,
		)
	}

	if cfg.KeepAlive.PingTimeout >= cfg.KeepAlive.PingTime {
		return fmt.Errorf(
			"keep_alive.ping_timeout (%s) must be less than ping_time (%s)",
			cfg.KeepAlive.PingTimeout, cfg.KeepAlive.PingTime,
		)
	}

	if cfg.KeepAlive.MinPingInterval >= cfg.KeepAlive.PingTime {
		return fmt.Errorf(
			"keep_alive.min_ping_interval (%s) must be less than ping_time (%s)",
			cfg.KeepAlive.MinPingInterval, cfg.KeepAlive.PingTime,
		)
	}

	return nil
}
