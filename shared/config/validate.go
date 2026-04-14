package config

import (
	"fmt"
	"net"
	"strings"
	"time"
)

func ValidateTCPAddress(fieldName, value string, allowEmptyHost bool) error {
	value = strings.TrimSpace(value)
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

func ValidateServiceConfig(fieldPrefix string, cfg ServiceConfig, allowEmptyHost bool) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("%s.name is required", fieldPrefix)
	}

	if cfg.MaxRecvMsgSize <= 0 {
		return fmt.Errorf(
			"%s.max_recv_msg_size must be greater than 0, got %d",
			fieldPrefix,
			cfg.MaxRecvMsgSize,
		)
	}

	if err := ValidateTCPAddress(fieldPrefix+".address", cfg.Address, allowEmptyHost); err != nil {
		return err
	}

	return nil
}

func ValidateHealthConfig(fieldPrefix string, cfg HealthConfig) error {
	if cfg.CheckInterval <= 0 {
		return fmt.Errorf(
			"%s.check_interval must be greater than 0, got %s",
			fieldPrefix,
			cfg.CheckInterval,
		)
	}

	if cfg.SuccessThreshold <= 0 {
		return fmt.Errorf(
			"%s.success_threshold must be greater than 0, got %d",
			fieldPrefix,
			cfg.SuccessThreshold,
		)
	}

	if cfg.FailureThreshold <= 0 {
		return fmt.Errorf(
			"%s.failure_threshold must be greater than 0, got %d",
			fieldPrefix,
			cfg.FailureThreshold,
		)
	}

	return nil
}

func ValidatePostgresPoolConfig(fieldPrefix string, cfg PostgresPoolConfig) error {
	if cfg.MaxConnections < 0 {
		return fmt.Errorf(
			"%s.max_conns must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.MaxConnections,
		)
	}

	if cfg.MinConnections < 0 {
		return fmt.Errorf(
			"%s.min_conns must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.MinConnections,
		)
	}

	if cfg.MaxConnLifetime < 0 {
		return fmt.Errorf(
			"%s.max_conn_lifetime must be greater than or equal to 0, got %s",
			fieldPrefix,
			cfg.MaxConnLifetime,
		)
	}

	if cfg.MaxConnIdleTime < 0 {
		return fmt.Errorf(
			"%s.max_conn_idle_time must be greater than or equal to 0, got %s",
			fieldPrefix,
			cfg.MaxConnIdleTime,
		)
	}

	if cfg.MaxConnections > 0 && cfg.MinConnections > cfg.MaxConnections {
		return fmt.Errorf(
			"%s.min_conns (%d) must be less than or equal to max_conns (%d)",
			fieldPrefix,
			cfg.MinConnections,
			cfg.MaxConnections,
		)
	}

	return nil
}

func ValidateMetricsConfig(fieldPrefix string, cfg MetricsConfig) error {
	if err := ValidateTCPAddress(fieldPrefix+".http_address", cfg.HTTPAddress, true); err != nil {
		return err
	}

	if cfg.ReadTimeout <= 0 {
		return fmt.Errorf(
			"%s.read_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.ReadTimeout,
		)
	}

	if cfg.WriteTimeout <= 0 {
		return fmt.Errorf(
			"%s.write_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.WriteTimeout,
		)
	}

	if cfg.IdleTimeout <= 0 {
		return fmt.Errorf(
			"%s.idle_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.IdleTimeout,
		)
	}

	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf(
			"%s.shutdown_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.ShutdownTimeout,
		)
	}

	return nil
}

func ValidateTracingConfig(fieldPrefix string, cfg TracingConfig) error {
	if err := ValidateTCPAddress(fieldPrefix+".exporter_otlp_endpoint", cfg.CollectorEndpoint, false); err != nil {
		return err
	}

	if strings.TrimSpace(cfg.Environment) == "" {
		return fmt.Errorf("%s.environment is required", fieldPrefix)
	}

	if strings.TrimSpace(cfg.ServiceVersion) == "" {
		return fmt.Errorf("%s.service_version is required", fieldPrefix)
	}

	return nil
}

func ValidateKeepAliveConfig(fieldPrefix string, cfg KeepAliveConfig) error {
	if cfg.PingTime <= 0 {
		return fmt.Errorf(
			"%s.ping_time must be greater than 0, got %s",
			fieldPrefix,
			cfg.PingTime,
		)
	}

	if cfg.PingTimeout <= 0 {
		return fmt.Errorf(
			"%s.ping_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.PingTimeout,
		)
	}

	if cfg.MinPingInterval <= 0 {
		return fmt.Errorf(
			"%s.min_ping_interval must be greater than 0, got %s",
			fieldPrefix,
			cfg.MinPingInterval,
		)
	}

	if cfg.PingTimeout >= cfg.PingTime {
		return fmt.Errorf(
			"%s.ping_timeout (%s) must be less than ping_time (%s)",
			fieldPrefix,
			cfg.PingTimeout,
			cfg.PingTime,
		)
	}

	if cfg.MinPingInterval >= cfg.PingTime {
		return fmt.Errorf(
			"%s.min_ping_interval (%s) must be less than ping_time (%s)",
			fieldPrefix,
			cfg.MinPingInterval,
			cfg.PingTime,
		)
	}

	return nil
}

func ValidateRedisBaseConfig(fieldPrefix string, cfg RedisConfig, serviceTimeout time.Duration) error {
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("%s.host is required", fieldPrefix)
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("%s.port must be between 1 and 65535, got %d", fieldPrefix, cfg.Port)
	}

	if cfg.ConnectionTimeout <= 0 {
		return fmt.Errorf(
			"%s.connection_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.ConnectionTimeout,
		)
	}

	if serviceTimeout > 0 && cfg.ConnectionTimeout >= serviceTimeout {
		return fmt.Errorf(
			"%s.connection_timeout (%s) must be less than service_timeout (%s)",
			fieldPrefix,
			cfg.ConnectionTimeout,
			serviceTimeout,
		)
	}

	if cfg.PoolSize < 0 {
		return fmt.Errorf("%s.pool_size must be greater than or equal to 0, got %d", fieldPrefix, cfg.PoolSize)
	}

	if cfg.MinIdle < 0 {
		return fmt.Errorf("%s.min_idle must be greater than or equal to 0, got %d", fieldPrefix, cfg.MinIdle)
	}

	if cfg.MaxIdle < 0 {
		return fmt.Errorf("%s.max_idle must be greater than or equal to 0, got %d", fieldPrefix, cfg.MaxIdle)
	}

	if cfg.MaxActiveConns < 0 {
		return fmt.Errorf(
			"%s.max_active_conns must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.MaxActiveConns,
		)
	}

	if cfg.IdleTimeout < 0 {
		return fmt.Errorf(
			"%s.idle_timeout must be greater than or equal to 0, got %s",
			fieldPrefix,
			cfg.IdleTimeout,
		)
	}

	if cfg.ConnMaxLifetime < 0 {
		return fmt.Errorf(
			"%s.max_conn_lifetime must be greater than or equal to 0, got %s",
			fieldPrefix,
			cfg.ConnMaxLifetime,
		)
	}

	return nil
}

func ValidateKafkaBrokers(fieldPrefix string, brokers []string) error {
	if len(brokers) == 0 {
		return fmt.Errorf("%s must contain at least one broker", fieldPrefix)
	}

	for i, broker := range brokers {
		if err := ValidateTCPAddress(
			fmt.Sprintf("%s[%d]", fieldPrefix, i),
			broker,
			false,
		); err != nil {
			return err
		}
	}

	return nil
}

func ValidateKafkaProducerConfig(fieldPrefix string, cfg ProducerConfig) error {
	if cfg.Timeout <= 0 {
		return fmt.Errorf(
			"%s.timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.Timeout,
		)
	}

	if cfg.RetryBackoff <= 0 {
		return fmt.Errorf(
			"%s.retry_backoff must be greater than 0, got %s",
			fieldPrefix,
			cfg.RetryBackoff,
		)
	}

	if cfg.MaxRetries < 0 {
		return fmt.Errorf(
			"%s.max_retries must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.MaxRetries,
		)
	}

	if cfg.ChannelBufferSize < 0 {
		return fmt.Errorf(
			"%s.channel_buffer_size must be >= 0, got %d",
			fieldPrefix,
			cfg.ChannelBufferSize,
		)
	}

	compression := NormalizeKafkaCompression(cfg.Compression)
	switch compression {
	case "none", "gzip", "snappy", "lz4", "zstd":
		return nil
	default:
		return fmt.Errorf(
			"%s.compression must be one of: none, gzip, snappy, lz4, zstd; got %q",
			fieldPrefix,
			cfg.Compression,
		)
	}
}

func NormalizeKafkaCompression(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func ValidateKafkaConsumerConfig(fieldPrefix string, cfg ConsumerConfig) error {
	if strings.TrimSpace(cfg.GroupID) == "" {
		return fmt.Errorf("%s.group_id is required", fieldPrefix)
	}

	if cfg.SessionTimeout <= 0 {
		return fmt.Errorf(
			"%s.session_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.SessionTimeout,
		)
	}

	if cfg.HeartbeatInterval <= 0 {
		return fmt.Errorf(
			"%s.heartbeat_interval must be greater than 0, got %s",
			fieldPrefix,
			cfg.HeartbeatInterval,
		)
	}

	if cfg.HeartbeatInterval >= cfg.SessionTimeout {
		return fmt.Errorf(
			"%s.heartbeat_interval (%s) must be less than session_timeout (%s)",
			fieldPrefix,
			cfg.HeartbeatInterval,
			cfg.SessionTimeout,
		)
	}

	if cfg.RetryBackoff <= 0 {
		return fmt.Errorf(
			"%s.retry_backoff must be greater than 0, got %s",
			fieldPrefix,
			cfg.RetryBackoff,
		)
	}

	if cfg.RetryJitter < 0 || cfg.RetryJitter > 1 {
		return fmt.Errorf(
			"%s.retry_jitter must be between 0 and 1 inclusive, got %v",
			fieldPrefix,
			cfg.RetryJitter,
		)
	}

	if cfg.MaxRetries < 0 {
		return fmt.Errorf(
			"%s.max_retries must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.MaxRetries,
		)
	}

	if cfg.MaxMessageBytes <= 0 {
		return fmt.Errorf(
			"%s.max_message_bytes must be greater than 0, got %d",
			fieldPrefix,
			cfg.MaxMessageBytes,
		)
	}

	if cfg.DLQMaxMessageBytes < 0 {
		return fmt.Errorf(
			"%s.dlq_max_message_bytes must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.DLQMaxMessageBytes,
		)
	}

	if cfg.DLQEnabled && cfg.DLQMaxMessageBytes <= 0 {
		return fmt.Errorf(
			"%s.dlq_max_message_bytes must be greater than 0 when dlq_enabled=true, got %d",
			fieldPrefix,
			cfg.DLQMaxMessageBytes,
		)
	}

	if cfg.RestartBackoff <= 0 {
		return fmt.Errorf(
			"%s.restart_backoff must be greater than 0, got %s",
			fieldPrefix,
			cfg.RestartBackoff,
		)
	}

	if cfg.DLQEnabled && cfg.DLQMaxMessageBytes < int(cfg.MaxMessageBytes) {
		return fmt.Errorf(
			"%s.dlq_max_message_bytes (%d) must be greater than or equal to %s.max_message_bytes (%d) when dlq_enabled=true",
			fieldPrefix,
			cfg.DLQMaxMessageBytes,
			fieldPrefix,
			cfg.MaxMessageBytes,
		)
	}

	return nil
}

func ValidateOutboxConfig(fieldPrefix string, cfg OutboxConfig) error {
	if cfg.PollInterval <= 0 {
		return fmt.Errorf(
			"%s.poll_interval must be greater than 0, got %s",
			fieldPrefix,
			cfg.PollInterval,
		)
	}

	if cfg.ProcessingTimeout <= 0 {
		return fmt.Errorf(
			"%s.processing_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.ProcessingTimeout,
		)
	}

	if cfg.BatchTimeout <= 0 {
		return fmt.Errorf(
			"%s.batch_timeout must be greater than 0, got %s",
			fieldPrefix,
			cfg.BatchTimeout,
		)
	}

	if cfg.BatchSize <= 0 {
		return fmt.Errorf(
			"%s.batch_size must be greater than 0, got %d",
			fieldPrefix,
			cfg.BatchSize,
		)
	}

	if cfg.MaxRetries < 0 {
		return fmt.Errorf(
			"%s.max_retries must be greater than or equal to 0, got %d",
			fieldPrefix,
			cfg.MaxRetries,
		)
	}

	if cfg.BatchTimeout >= cfg.ProcessingTimeout {
		return fmt.Errorf(
			"%s.batch_timeout (%s) must be less than processing_timeout (%s)",
			fieldPrefix,
			cfg.BatchTimeout,
			cfg.ProcessingTimeout,
		)
	}

	return nil
}
