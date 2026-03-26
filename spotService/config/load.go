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

	cfg.Service.DBURI = os.Getenv("SPOT_DB_URI")
	if cfg.Service.DBURI == "" {
		return nil, errors.New("SPOT_DB_URI is required")
	}

	if err := validateSpotConfig(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateSpotConfig(cfg config.SpotConfig) error {
	if err := validateSpotTimeouts(cfg); err != nil {
		return err
	}

	return nil
}

func validateSpotTimeouts(cfg config.SpotConfig) error {
	if cfg.Timeouts.Service <= 0 {
		return fmt.Errorf(
			"timeouts.service must be greater than 0, got %s",
			cfg.Timeouts.Service,
		)
	}

	if cfg.Redis.ConnectionTimeout <= 0 {
		return fmt.Errorf(
			"redis.connection_timeout must be greater than 0, got %s",
			cfg.Redis.ConnectionTimeout,
		)
	}

	if cfg.KeepAlive.PingTime <= 0 {
		return fmt.Errorf(
			"keep_alive.ping_time must be greater than 0, got %s",
			cfg.KeepAlive.PingTime,
		)
	}

	if cfg.KeepAlive.PingTimeout <= 0 {
		return fmt.Errorf(
			"keep_alive.ping_timeout must be greater than 0, got %s",
			cfg.KeepAlive.PingTimeout,
		)
	}

	if cfg.KeepAlive.MinPingInterval <= 0 {
		return fmt.Errorf(
			"keep_alive.min_ping_interval must be greater than 0, got %s",
			cfg.KeepAlive.MinPingInterval,
		)
	}

	if cfg.Redis.ConnectionTimeout >= cfg.Timeouts.Service {
		return fmt.Errorf(
			"redis.connection_timeout (%s) must be less than service_timeout (%s)",
			cfg.Redis.ConnectionTimeout, cfg.Timeouts.Service,
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
