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

	cfg.Auth.JWTSecret = os.Getenv("JWT_SECRET")
	if cfg.Auth.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	if err := validateOrderConfig(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateOrderConfig(cfg config.OrderConfig) error {
	if err := validateOrderTimeouts(cfg); err != nil {
		return err
	}

	if err := validateOutboxConfig(cfg.Kafka.Outbox); err != nil {
		return err
	}

	return nil
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

func validateOutboxConfig(cfg config.OutboxConfig) error {
	if cfg.PollInterval <= 0 {
		return fmt.Errorf("kafka.outbox.poll_interval must be greater than 0, got %s", cfg.PollInterval)
	}

	if cfg.ProcessingTimeout <= 0 {
		return fmt.Errorf("kafka.outbox.processing_timeout must be greater than 0, got %s", cfg.ProcessingTimeout)
	}

	if cfg.BatchTimeout <= 0 {
		return fmt.Errorf("kafka.outbox.batch_timeout must be greater than 0, got %s", cfg.BatchTimeout)
	}

	if cfg.BatchSize <= 0 {
		return fmt.Errorf("kafka.outbox.batch_size must be greater than 0, got %d", cfg.BatchSize)
	}

	if cfg.MaxRetries < 0 {
		return fmt.Errorf("kafka.outbox.max_retries must be greater than or equal to 0, got %d", cfg.MaxRetries)
	}

	if cfg.BatchTimeout >= cfg.ProcessingTimeout {
		return fmt.Errorf(
			"kafka.outbox.batch_timeout (%s) must be less than processing_timeout (%s)",
			cfg.BatchTimeout, cfg.ProcessingTimeout,
		)
	}

	return nil
}
