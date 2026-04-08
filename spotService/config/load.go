package config

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/spf13/viper"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

const configDir = "."

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

	cfg.AuthVerifier.JWTSecret = os.Getenv("JWT_SECRET")
	if cfg.AuthVerifier.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	cfg.Kafka.Producer.Compression = config.NormalizeKafkaCompression(cfg.Kafka.Producer.Compression)

	if err := validateSpotConfig(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateSpotConfig(cfg config.SpotConfig) error {
	if err := validateSpotService(cfg); err != nil {
		return err
	}
	if err := config.ValidateHealthConfig("health", cfg.Health); err != nil {
		return err
	}
	if err := config.ValidatePostgresPoolConfig("postgres_pool", cfg.PostgresPool); err != nil {
		return err
	}
	if err := validateSpotViewMarkets(cfg); err != nil {
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
	if err := config.ValidateTracingConfig("tracing", cfg.Tracing); err != nil {
		return err
	}
	if err := config.ValidateMetricsConfig("metrics", cfg.Metrics); err != nil {
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

	if cfg.Timeouts.Check <= 0 {
		return fmt.Errorf(
			"timeouts.check must be greater than 0, got %s",
			cfg.Timeouts.Check,
		)
	}

	if err := config.ValidateServiceConfig("service", cfg.Service, true); err != nil {
		return err
	}

	return nil
}

func validateSpotViewMarkets(cfg config.SpotConfig) error {
	maxInt := uint64(math.MaxInt)

	if cfg.ViewMarkets.DefaultLimit <= 0 {
		return fmt.Errorf(
			"view_markets.default_limit must be greater than 0, got %d",
			cfg.ViewMarkets.DefaultLimit,
		)
	}
	if cfg.ViewMarkets.MaxLimit <= 0 {
		return fmt.Errorf(
			"view_markets.max_limit must be greater than 0, got %d",
			cfg.ViewMarkets.MaxLimit,
		)
	}
	if cfg.ViewMarkets.CacheLimit <= 0 {
		return fmt.Errorf(
			"view_markets.cache_limit must be greater than 0, got %d",
			cfg.ViewMarkets.CacheLimit,
		)
	}
	if cfg.ViewMarkets.DefaultLimit > maxInt {
		return fmt.Errorf(
			"view_markets.default_limit must be less than or equal to %d, got %d",
			maxInt,
			cfg.ViewMarkets.DefaultLimit,
		)
	}
	if cfg.ViewMarkets.MaxLimit > maxInt {
		return fmt.Errorf(
			"view_markets.max_limit must be less than or equal to %d, got %d",
			maxInt,
			cfg.ViewMarkets.MaxLimit,
		)
	}
	if cfg.ViewMarkets.CacheLimit > maxInt {
		return fmt.Errorf(
			"view_markets.cache_limit must be less than or equal to %d, got %d",
			maxInt,
			cfg.ViewMarkets.CacheLimit,
		)
	}
	if cfg.ViewMarkets.DefaultLimit > cfg.ViewMarkets.MaxLimit {
		return fmt.Errorf(
			"view_markets.default_limit must be less than or equal to view_markets.max_limit, "+
				"got default_limit=%d max_limit=%d",
			cfg.ViewMarkets.DefaultLimit,
			cfg.ViewMarkets.MaxLimit,
		)
	}
	if cfg.ViewMarkets.CacheLimit < cfg.ViewMarkets.DefaultLimit {
		return fmt.Errorf(
			"view_markets.cache_limit must be greater than or equal to view_markets.default_limit, "+
				"got cache_limit=%d default_limit=%d",
			cfg.ViewMarkets.CacheLimit,
			cfg.ViewMarkets.DefaultLimit,
		)
	}

	return nil
}

func validateSpotRedis(cfg config.SpotConfig) error {
	if err := config.ValidateRedisBaseConfig("redis", cfg.Redis, cfg.Timeouts.Service); err != nil {
		return err
	}

	if cfg.Redis.CacheTTL <= 0 {
		return fmt.Errorf(
			"redis.spot_cache_ttl must be greater than 0, got %s",
			cfg.Redis.CacheTTL,
		)
	}

	return nil
}

func validateSpotKeepAlive(cfg config.SpotConfig) error {
	return config.ValidateKeepAliveConfig("keep_alive", cfg.KeepAlive)
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

	if cfg.MarketPoller.RestartBackoff <= 0 {
		return fmt.Errorf(
			"market_poller.restart_backoff must be greater than 0, got %s",
			cfg.MarketPoller.RestartBackoff,
		)
	}

	return nil
}

func validateSpotKafka(cfg config.SpotConfig) error {
	if err := config.ValidateKafkaBrokers("kafka.brokers", cfg.Kafka.Brokers); err != nil {
		return err
	}

	if cfg.Kafka.Topics.MarketStateChanged == "" {
		return errors.New("kafka.topics.market_state_changed is required")
	}

	if err := config.ValidateKafkaProducerConfig("kafka.producer", cfg.Kafka.Producer); err != nil {
		return err
	}

	if err := config.ValidateOutboxConfig("kafka.outbox", cfg.Kafka.Outbox); err != nil {
		return err
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
