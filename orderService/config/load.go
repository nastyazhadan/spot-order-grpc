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
	if err := validateOrderKafka(cfg); err != nil {
		return err
	}
	if err := validateOutboxConfig(cfg.Kafka.Outbox); err != nil {
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

	if cfg.SpotAddress == "" {
		return errors.New("spot_address is required")
	}

	return nil
}

func validateOrderRedis(cfg config.OrderConfig) error {
	if cfg.Redis.ConnectionTimeout <= 0 {
		return fmt.Errorf(
			"redis.connection_timeout must be greater than 0, got %s",
			cfg.Redis.ConnectionTimeout,
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

func validateOrderAuth(cfg config.OrderConfig) error {
	if cfg.Auth.AccessTokenTTL <= 0 {
		return fmt.Errorf(
			"auth.access_token_ttl must be greater than 0, got %s",
			cfg.Auth.AccessTokenTTL,
		)
	}

	if cfg.Auth.RefreshTokenTTL <= 0 {
		return fmt.Errorf(
			"auth.refresh_token_ttl must be greater than 0, got %s",
			cfg.Auth.RefreshTokenTTL,
		)
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
	if len(cfg.Kafka.Brokers) == 0 {
		return errors.New("kafka.brokers must contain at least one broker")
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

	if cfg.Kafka.Producer.Timeout <= 0 {
		return fmt.Errorf(
			"kafka.producer.timeout must be greater than 0, got %s",
			cfg.Kafka.Producer.Timeout,
		)
	}

	if cfg.Kafka.Producer.RetryBackoff <= 0 {
		return fmt.Errorf(
			"kafka.producer.retry_backoff must be greater than 0, got %s",
			cfg.Kafka.Producer.RetryBackoff,
		)
	}

	if cfg.Kafka.Producer.MaxRetries < 0 {
		return fmt.Errorf(
			"kafka.producer.max_retries must be greater than or equal to 0, got %d",
			cfg.Kafka.Producer.MaxRetries,
		)
	}

	if cfg.Kafka.Consumer.GroupID == "" {
		return errors.New("kafka.consumer.group_id is required")
	}

	if cfg.Kafka.Consumer.SessionTimeout <= 0 {
		return fmt.Errorf(
			"kafka.consumer.session_timeout must be greater than 0, got %s",
			cfg.Kafka.Consumer.SessionTimeout,
		)
	}

	if cfg.Kafka.Consumer.HeartbeatInterval <= 0 {
		return fmt.Errorf(
			"kafka.consumer.heartbeat_interval must be greater than 0, got %s",
			cfg.Kafka.Consumer.HeartbeatInterval,
		)
	}

	if cfg.Kafka.Consumer.HeartbeatInterval >= cfg.Kafka.Consumer.SessionTimeout {
		return fmt.Errorf(
			"kafka.consumer.heartbeat_interval (%s) must be less than session_timeout (%s)",
			cfg.Kafka.Consumer.HeartbeatInterval,
			cfg.Kafka.Consumer.SessionTimeout,
		)
	}

	if cfg.Kafka.Consumer.RetryBackoff <= 0 {
		return fmt.Errorf(
			"kafka.consumer.retry_backoff must be greater than 0, got %s",
			cfg.Kafka.Consumer.RetryBackoff,
		)
	}

	if cfg.Kafka.Consumer.MaxRetries < 0 {
		return fmt.Errorf(
			"kafka.consumer.max_retries must be greater than or equal to 0, got %d",
			cfg.Kafka.Consumer.MaxRetries,
		)
	}

	return nil
}

func validateOutboxConfig(cfg config.OutboxConfig) error {
	if cfg.PollInterval <= 0 {
		return fmt.Errorf(
			"kafka.outbox.poll_interval must be greater than 0, got %s",
			cfg.PollInterval,
		)
	}

	if cfg.ProcessingTimeout <= 0 {
		return fmt.Errorf(
			"kafka.outbox.processing_timeout must be greater than 0, got %s",
			cfg.ProcessingTimeout,
		)
	}

	if cfg.BatchTimeout <= 0 {
		return fmt.Errorf(
			"kafka.outbox.batch_timeout must be greater than 0, got %s",
			cfg.BatchTimeout,
		)
	}

	if cfg.BatchSize <= 0 {
		return fmt.Errorf(
			"kafka.outbox.batch_size must be greater than 0, got %d",
			cfg.BatchSize,
		)
	}

	if cfg.MaxRetries < 0 {
		return fmt.Errorf(
			"kafka.outbox.max_retries must be greater than or equal to 0, got %d",
			cfg.MaxRetries,
		)
	}

	if cfg.BatchTimeout >= cfg.ProcessingTimeout {
		return fmt.Errorf(
			"kafka.outbox.batch_timeout (%s) must be less than processing_timeout (%s)",
			cfg.BatchTimeout,
			cfg.ProcessingTimeout,
		)
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
