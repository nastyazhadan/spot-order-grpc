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

	cfg.Service.DBURI = os.Getenv("ORDER_DB_URI")
	if cfg.Service.DBURI == "" {
		return nil, errors.New("ORDER_DB_URI is required")
	}

	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	if cfg.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	if err := validateOrderTimeouts(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateOrderTimeouts(cfg config.OrderConfig) error {
	if cfg.Redis.ConnectionTimeout >= cfg.Timeouts.Service {
		return fmt.Errorf(
			"redis.connection_timeout (%s) must be less than service_timeout (%s)",
			cfg.Redis.ConnectionTimeout, cfg.Timeouts.Service,
		)
	}

	if cfg.CircuitBreaker.Timeout >= cfg.Timeouts.Service {
		return fmt.Errorf(
			"breaker.timeout (%s) must be less than service_timeout (%s)",
			cfg.CircuitBreaker.Timeout, cfg.Timeouts.Service,
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
