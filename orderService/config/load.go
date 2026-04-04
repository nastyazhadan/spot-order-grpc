package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/spf13/viper"
)

const configDir = "."

func Load() (*config.OrderConfig, error) {
	if err := config.LoadAll(configDir); err != nil {
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

	cfg.Kafka.Producer.Compression = config.NormalizeKafkaCompression(cfg.Kafka.Producer.Compression)

	if err := validateOrderConfig(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateOrderConfig(cfg config.OrderConfig) error {
	if err := validateOrderService(cfg); err != nil {
		return err
	}
	if err := validateOrderRedis(cfg); err != nil {
		return err
	}
	if err := validateOrderCircuitBreaker(cfg); err != nil {
		return err
	}
	if err := validateOrderKeepAlive(cfg); err != nil {
		return err
	}
	if err := validateOrderAuth(cfg); err != nil {
		return err
	}
	if err := validateOrderRetry(cfg); err != nil {
		return err
	}
	if err := validateOrderRateLimits(cfg); err != nil {
		return err
	}
	if err := config.ValidateTracingConfig("tracing", cfg.Tracing); err != nil {
		return err
	}
	if err := config.ValidateMetricsConfig("metrics", cfg.Metrics); err != nil {
		return err
	}
	if err := validateOrderKafka(cfg); err != nil {
		return err
	}

	return nil
}

func validateOrderService(cfg config.OrderConfig) error {
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

	if err := config.ValidateTCPAddress("spot_address", cfg.SpotAddress, false); err != nil {
		return err
	}

	return nil
}

func validateOrderRedis(cfg config.OrderConfig) error {
	if err := config.ValidateRedisBaseConfig("redis", cfg.Redis, cfg.Timeouts.Service); err != nil {
		return err
	}

	if cfg.Redis.MarketBlockTTL <= 0 {
		return fmt.Errorf(
			"redis.market_block_ttl must be greater than 0, got %s",
			cfg.Redis.MarketBlockTTL,
		)
	}

	return nil
}

func validateOrderCircuitBreaker(cfg config.OrderConfig) error {
	if cfg.CircuitBreaker.Interval <= 0 {
		return fmt.Errorf(
			"circuit_breaker.interval must be greater than 0, got %s",
			cfg.CircuitBreaker.Interval,
		)
	}

	if cfg.CircuitBreaker.Timeout <= 0 {
		return fmt.Errorf(
			"circuit_breaker.timeout must be greater than 0, got %s",
			cfg.CircuitBreaker.Timeout,
		)
	}

	if cfg.CircuitBreaker.Timeout >= cfg.Timeouts.Service {
		return fmt.Errorf(
			"circuit_breaker.timeout (%s) must be less than service_timeout (%s)",
			cfg.CircuitBreaker.Timeout,
			cfg.Timeouts.Service,
		)
	}

	if cfg.CircuitBreaker.MaxFailures == 0 {
		return errors.New("circuit_breaker.max_failures must be greater than 0")
	}

	return nil
}

func validateOrderKeepAlive(cfg config.OrderConfig) error {
	return config.ValidateKeepAliveConfig("keep_alive", cfg.KeepAlive)
}

func validateOrderAuth(cfg config.OrderConfig) error {
	if err := config.ValidateAuthConfig("auth", cfg.Auth); err != nil {
		return err
	}

	if cfg.Auth.RefreshTokenTTL <= cfg.Auth.AccessTokenTTL {
		return fmt.Errorf(
			"auth.refresh_token_ttl (%s) must be greater than access_token_ttl (%s)",
			cfg.Auth.RefreshTokenTTL,
			cfg.Auth.AccessTokenTTL,
		)
	}

	return nil
}

func validateOrderRetry(cfg config.OrderConfig) error {
	if cfg.Retry.MaxAttempts == 0 {
		return errors.New("retry.max_attempts must be greater than 0")
	}

	if cfg.Retry.InitialBackoff <= 0 {
		return fmt.Errorf(
			"retry.initial_backoff must be greater than 0, got %s",
			cfg.Retry.InitialBackoff,
		)
	}

	if cfg.Retry.Jitter < 0 || cfg.Retry.Jitter > 1 {
		return fmt.Errorf(
			"retry.jitter must be between 0 and 1 inclusive, got %v",
			cfg.Retry.Jitter,
		)
	}

	if cfg.Retry.PerRetryTimeout <= 0 {
		return fmt.Errorf(
			"retry.per_retry_timeout must be greater than 0, got %s",
			cfg.Retry.PerRetryTimeout,
		)
	}

	if cfg.Retry.PerRetryTimeout >= cfg.Timeouts.Service {
		return fmt.Errorf(
			"retry.per_retry_timeout (%s) must be less than service_timeout (%s)",
			cfg.Retry.PerRetryTimeout,
			cfg.Timeouts.Service,
		)
	}

	return nil
}

func validateOrderKafka(cfg config.OrderConfig) error {
	if err := config.ValidateKafkaBrokers("kafka.brokers", cfg.Kafka.Brokers); err != nil {
		return err
	}

	if cfg.Kafka.Topics.OrderCreated == "" {
		return errors.New("kafka.topics.order_created is required")
	}

	if cfg.Kafka.Topics.OrderStatusUpdated == "" {
		return errors.New("kafka.topics.order_status_updated is required")
	}

	if cfg.Kafka.Topics.MarketStateChanged == "" {
		return errors.New("kafka.topics.market_state_changed is required")
	}

	if cfg.Kafka.Consumer.DLQEnabled && cfg.Kafka.Topics.MarketStateChangedDLQ == "" {
		return errors.New("kafka.topics.market_state_changed_dlq is required when dlq_enabled=true")
	}

	if err := config.ValidateKafkaProducerConfig("kafka.producer", cfg.Kafka.Producer); err != nil {
		return err
	}

	if err := config.ValidateKafkaConsumerConfig("kafka.consumer", cfg.Kafka.Consumer); err != nil {
		return err
	}

	if err := config.ValidateOutboxConfig("kafka.outbox", cfg.Kafka.Outbox); err != nil {
		return err
	}

	return nil
}

func validateOrderRateLimits(cfg config.OrderConfig) error {
	if cfg.GRPCRateLimit.CreateOrder <= 0 {
		return fmt.Errorf(
			"grpc_rate_limit.create_order must be greater than 0, got %d",
			cfg.GRPCRateLimit.CreateOrder,
		)
	}

	if cfg.GRPCRateLimit.GetOrderStatus <= 0 {
		return fmt.Errorf(
			"grpc_rate_limit.get_order_status must be greater than 0, got %d",
			cfg.GRPCRateLimit.GetOrderStatus,
		)
	}

	if cfg.GRPCRateLimit.RefreshToken <= 0 {
		return fmt.Errorf(
			"grpc_rate_limit.refresh_token must be greater than 0, got %d",
			cfg.GRPCRateLimit.RefreshToken,
		)
	}

	if cfg.RateLimitByUser.CreateOrder <= 0 {
		return fmt.Errorf(
			"rate_limit_by_user.create_order must be greater than 0, got %d",
			cfg.RateLimitByUser.CreateOrder,
		)
	}

	if cfg.RateLimitByUser.GetOrderStatus <= 0 {
		return fmt.Errorf(
			"rate_limit_by_user.get_order_status must be greater than 0, got %d",
			cfg.RateLimitByUser.GetOrderStatus,
		)
	}

	if cfg.RateLimitByUser.Window <= 0 {
		return fmt.Errorf(
			"rate_limit_by_user.window must be greater than 0, got %s",
			cfg.RateLimitByUser.Window,
		)
	}

	return nil
}
