package config

import (
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/spf13/viper"
)

const configDir = ""

func Load() (*config.SpotConfig, error) {
	if err := config.LoadAll(configDir); err != nil {
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
	if err := validateSpotService(cfg); err != nil {
		return err
	}
	if err := validateSpotRedis(cfg); err != nil {
		return err
	}
	if err := validateSpotKeepAlive(cfg); err != nil {
		return err
	}
	if err := validateSpotRateLimits(cfg); err != nil {
		return err
	}
	if err := validateSpotMarketPoller(cfg); err != nil {
		return err
	}
	if err := validateSpotKafka(cfg); err != nil {
		return err
	}

	return nil
}

func validateSpotService(cfg config.SpotConfig) error {
	if cfg.Timeouts.Service <= 0 {
		return fmt.Errorf(
			"timeouts.service must be greater than 0, got %s",
			cfg.Timeouts.Service,
		)
	}

	if err := validateTCPAddress("service.address", cfg.Service.Address, true); err != nil {
		return err
	}

	if err := validateTCPAddress("metrics.http_address", cfg.Metrics.HTTPAddress, true); err != nil {
		return err
	}

	return nil
}

func validateSpotRedis(cfg config.SpotConfig) error {
	if cfg.Redis.ConnectionTimeout <= 0 {
		return fmt.Errorf(
			"redis.connection_timeout must be greater than 0, got %s",
			cfg.Redis.ConnectionTimeout,
		)
	}

	if cfg.Redis.CacheTTL <= 0 {
		return fmt.Errorf(
			"redis.spot_cache_ttl must be greater than 0, got %s",
			cfg.Redis.CacheTTL,
		)
	}

	if cfg.Redis.ConnectionTimeout >= cfg.Timeouts.Service {
		return fmt.Errorf(
			"redis.connection_timeout (%s) must be less than service_timeout (%s)",
			cfg.Redis.ConnectionTimeout,
			cfg.Timeouts.Service,
		)
	}

	return nil
}

func validateSpotKeepAlive(cfg config.SpotConfig) error {
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

	if cfg.KeepAlive.PingTimeout >= cfg.KeepAlive.PingTime {
		return fmt.Errorf(
			"keep_alive.ping_timeout (%s) must be less than ping_time (%s)",
			cfg.KeepAlive.PingTimeout,
			cfg.KeepAlive.PingTime,
		)
	}

	if cfg.KeepAlive.MinPingInterval >= cfg.KeepAlive.PingTime {
		return fmt.Errorf(
			"keep_alive.min_ping_interval (%s) must be less than ping_time (%s)",
			cfg.KeepAlive.MinPingInterval,
			cfg.KeepAlive.PingTime,
		)
	}

	return nil
}

func validateSpotMarketPoller(cfg config.SpotConfig) error {
	if cfg.MarketPoller.PollInterval <= 0 {
		return fmt.Errorf(
			"market_poller.poll_interval must be greater than 0, got %s",
			cfg.MarketPoller.PollInterval,
		)
	}

	if cfg.MarketPoller.ProcessingTimeout <= 0 {
		return fmt.Errorf(
			"market_poller.processing_timeout must be greater than 0, got %s",
			cfg.MarketPoller.ProcessingTimeout,
		)
	}

	if cfg.MarketPoller.BatchSize <= 0 {
		return fmt.Errorf(
			"market_poller.batch_size must be greater than 0, got %d",
			cfg.MarketPoller.BatchSize,
		)
	}

	if cfg.MarketPoller.ProcessingTimeout < cfg.MarketPoller.PollInterval {
		return fmt.Errorf(
			"market_poller.processing_timeout (%s) must be greater than or equal to market_poller.poll_interval (%s)",
			cfg.MarketPoller.ProcessingTimeout,
			cfg.MarketPoller.PollInterval,
		)
	}

	return nil
}

func validateSpotKafka(cfg config.SpotConfig) error {
	if len(cfg.Kafka.Brokers) == 0 {
		return errors.New("kafka.brokers must contain at least one broker")
	}

	if cfg.Kafka.Topics.MarketStateChanged == "" {
		return errors.New("kafka.topics.market_state_changed is required")
	}

	if cfg.Kafka.Producer.Timeout <= 0 {
		return fmt.Errorf(
			"kafka.producer.timeout must be greater than 0, got %s",
			cfg.Kafka.Producer.Timeout,
		)
	}

	if cfg.Kafka.Producer.MaxRetries < 0 {
		return fmt.Errorf(
			"kafka.producer.max_retries must be greater than or equal to 0, got %d",
			cfg.Kafka.Producer.MaxRetries,
		)
	}

	if cfg.Kafka.Producer.RetryBackoff <= 0 {
		return fmt.Errorf(
			"kafka.producer.retry_backoff must be greater than 0, got %s",
			cfg.Kafka.Producer.RetryBackoff,
		)
	}

	if cfg.Kafka.Outbox.PollInterval <= 0 {
		return fmt.Errorf(
			"kafka.outbox.poll_interval must be greater than 0, got %s",
			cfg.Kafka.Outbox.PollInterval,
		)
	}

	if cfg.Kafka.Outbox.BatchSize <= 0 {
		return fmt.Errorf(
			"kafka.outbox.batch_size must be greater than 0, got %d",
			cfg.Kafka.Outbox.BatchSize,
		)
	}

	if cfg.Kafka.Outbox.BatchTimeout <= 0 {
		return fmt.Errorf(
			"kafka.outbox.batch_timeout must be greater than 0, got %s",
			cfg.Kafka.Outbox.BatchTimeout,
		)
	}

	if cfg.Kafka.Outbox.MaxRetries < 0 {
		return fmt.Errorf(
			"kafka.outbox.max_retries must be greater than or equal to 0, got %d",
			cfg.Kafka.Outbox.MaxRetries,
		)
	}

	if cfg.Kafka.Outbox.ProcessingTimeout <= 0 {
		return fmt.Errorf("kafka.outbox.processing_timeout must be greater than 0, got %s",
			cfg.Kafka.Outbox.ProcessingTimeout)
	}

	if cfg.Kafka.Outbox.BatchTimeout >= cfg.Kafka.Outbox.ProcessingTimeout {
		return fmt.Errorf(
			"kafka.outbox.batch_timeout (%s) must be less than processing_timeout (%s)",
			cfg.Kafka.Outbox.BatchTimeout,
			cfg.Kafka.Outbox.ProcessingTimeout,
		)
	}

	return nil
}

func validateSpotRateLimits(cfg config.SpotConfig) error {
	if cfg.GRPCRateLimit.ViewMarkets <= 0 {
		return fmt.Errorf(
			"grpc_rate_limit.view_markets must be greater than 0, got %d",
			cfg.GRPCRateLimit.ViewMarkets,
		)
	}

	if cfg.GRPCRateLimit.GetMarketByID <= 0 {
		return fmt.Errorf(
			"grpc_rate_limit.get_market_by_id must be greater than 0, got %d",
			cfg.GRPCRateLimit.GetMarketByID,
		)
	}

	return nil
}

func validateTCPAddress(fieldName, value string, allowEmptyHost bool) error {
	if value == "" {
		return fmt.Errorf("%s is required", fieldName)
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid host:port, got %q: %w", fieldName, value, err)
	}

	if !allowEmptyHost && host == "" {
		return fmt.Errorf("%s must include host, got %q", fieldName, value)
	}

	if port == "" {
		return fmt.Errorf("%s must include port, got %q", fieldName, value)
	}

	return nil
}
