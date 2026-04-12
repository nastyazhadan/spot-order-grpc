package config

import (
	"fmt"
	"time"
)

type OrderConfig struct {
	Service         ServiceConfig            `mapstructure:"service"`
	SpotAddress     string                   `mapstructure:"spot_address"`
	Timeouts        TimeoutsConfig           `mapstructure:"timeouts"`
	Log             LoggingConfig            `mapstructure:"log"`
	AuthVerifier    AuthVerifierConfig       `mapstructure:"auth_verifier"`
	Health          HealthConfig             `mapstructure:"health"`
	AuthIssuer      AuthIssuerConfig         `mapstructure:"auth_issuer"`
	CircuitBreaker  CircuitBreakerConfig     `mapstructure:"circuit_breaker"`
	PostgresPool    PostgresPoolConfig       `mapstructure:"postgres_pool"`
	GRPCRateLimit   OrderGRPCRateLimitConfig `mapstructure:"grpc_rate_limit"`
	RateLimitByUser RateLimiterByUserConfig  `mapstructure:"rate_limit_by_user"`
	Redis           RedisConfig              `mapstructure:"redis"`
	Tracing         TracingConfig            `mapstructure:"tracing"`
	Metrics         MetricsConfig            `mapstructure:"metrics"`
	KeepAlive       KeepAliveConfig          `mapstructure:"keep_alive"`
	Retry           RetryConfig              `mapstructure:"retry"`
	Kafka           KafkaConfig              `mapstructure:"kafka"`
}

type SpotConfig struct {
	Service       ServiceConfig           `mapstructure:"service"`
	ViewMarkets   ViewMarketsConfig       `mapstructure:"view_markets"`
	Log           LoggingConfig           `mapstructure:"log"`
	AuthVerifier  AuthVerifierConfig      `mapstructure:"auth_verifier"`
	Timeouts      TimeoutsConfig          `mapstructure:"timeouts"`
	Health        HealthConfig            `mapstructure:"health"`
	PostgresPool  PostgresPoolConfig      `mapstructure:"postgres_pool"`
	GRPCRateLimit SpotGRPCRateLimitConfig `mapstructure:"grpc_rate_limit"`
	Redis         RedisConfig             `mapstructure:"redis"`
	Tracing       TracingConfig           `mapstructure:"tracing"`
	Metrics       MetricsConfig           `mapstructure:"metrics"`
	KeepAlive     KeepAliveConfig         `mapstructure:"keep_alive"`
	Kafka         KafkaConfig             `mapstructure:"kafka"`
	MarketPoller  MarketPollerConfig      `mapstructure:"market_poller"`
}

type ServiceConfig struct {
	Address        string `mapstructure:"address"`
	Name           string `mapstructure:"name"`
	MaxRecvMsgSize int    `mapstructure:"max_recv_msg_size"`
	DBURI          string `mapstructure:"db_uri"`
}

type ViewMarketsConfig struct {
	DefaultLimit uint64 `mapstructure:"default_limit"`
	MaxLimit     uint64 `mapstructure:"max_limit"`
	CacheLimit   uint64 `mapstructure:"cache_limit"`
}

type LoggingConfig struct {
	Level            string `mapstructure:"level"`
	Format           string `mapstructure:"format"`
	ContextFieldsMax int    `mapstructure:"context_fields_max"`
}

type TimeoutsConfig struct {
	Service time.Duration `mapstructure:"service"`
	Check   time.Duration `mapstructure:"check"`
}

type HealthConfig struct {
	CheckInterval    time.Duration `mapstructure:"check_interval"`
	SuccessThreshold int           `mapstructure:"success_threshold"`
	FailureThreshold int           `mapstructure:"failure_threshold"`
}

type CircuitBreakerConfig struct {
	MaxRequests uint32        `mapstructure:"max_requests"`
	Interval    time.Duration `mapstructure:"interval"`
	Timeout     time.Duration `mapstructure:"timeout"`
	MaxFailures uint32        `mapstructure:"max_failures"`
}

type PostgresPoolConfig struct {
	MaxConnections  int32         `mapstructure:"max_conns"`
	MinConnections  int32         `mapstructure:"min_conns"`
	MaxConnLifetime time.Duration `mapstructure:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `mapstructure:"max_conn_idle_time"`
}

type RedisConfig struct {
	Host              string        `mapstructure:"host"`
	Port              int           `mapstructure:"port"`
	ConnectionTimeout time.Duration `mapstructure:"connection_timeout"`
	PoolSize          int           `mapstructure:"pool_size"`
	MinIdle           int           `mapstructure:"min_idle"`
	MaxIdle           int           `mapstructure:"max_idle"`
	MaxActiveConns    int           `mapstructure:"max_active_conns"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ConnMaxLifetime   time.Duration `mapstructure:"max_conn_lifetime"`
	CacheTTL          time.Duration `mapstructure:"spot_cache_ttl"`
	MarketBlockTTL    time.Duration `mapstructure:"market_block_ttl"`
	IdempotencyTTL    time.Duration `mapstructure:"idempotency_ttl"`
}

type KafkaConfig struct {
	Brokers  []string       `mapstructure:"brokers"`
	Producer ProducerConfig `mapstructure:"producer"`
	Consumer ConsumerConfig `mapstructure:"consumer"`
	Topics   TopicsConfig   `mapstructure:"topics"`
	Outbox   OutboxConfig   `mapstructure:"outbox"`
}

type ProducerConfig struct {
	MaxRetries   int           `mapstructure:"max_retries"`
	RetryBackoff time.Duration `mapstructure:"retry_backoff"`
	Timeout      time.Duration `mapstructure:"timeout"`
	Compression  string        `mapstructure:"compression"`
}

type ConsumerConfig struct {
	GroupID            string        `mapstructure:"group_id"`
	SessionTimeout     time.Duration `mapstructure:"session_timeout"`
	HeartbeatInterval  time.Duration `mapstructure:"heartbeat_interval"`
	MaxRetries         int           `mapstructure:"max_retries"`
	RetryBackoff       time.Duration `mapstructure:"retry_backoff"`
	RetryJitter        float64       `mapstructure:"retry_jitter"`
	MaxMessageBytes    int32         `mapstructure:"max_message_bytes"`
	DLQEnabled         bool          `mapstructure:"dlq_enabled"`
	DLQMaxMessageBytes int           `mapstructure:"dlq_max_message_bytes"`
	RestartBackoff     time.Duration `mapstructure:"restart_backoff"`
}

type TopicsConfig struct {
	OrderCreated          string `mapstructure:"order_created"`
	OrderStatusUpdated    string `mapstructure:"order_status_updated"`
	MarketStateChanged    string `mapstructure:"market_state_changed"`
	MarketStateChangedDLQ string `mapstructure:"market_state_changed_dlq"`
}

type OutboxConfig struct {
	PollInterval      time.Duration `mapstructure:"poll_interval"`
	BatchSize         int           `mapstructure:"batch_size"`
	BatchTimeout      time.Duration `mapstructure:"batch_timeout"`
	MaxRetries        int           `mapstructure:"max_retries"`
	ProcessingTimeout time.Duration `mapstructure:"processing_timeout"`
}

type MarketPollerConfig struct {
	PollInterval      time.Duration `mapstructure:"poll_interval"`
	ProcessingTimeout time.Duration `mapstructure:"processing_timeout"`
	BatchSize         int           `mapstructure:"batch_size"`
	RestartBackoff    time.Duration `mapstructure:"restart_backoff"`
}

type RateLimiterByUserConfig struct {
	CreateOrder    int64         `mapstructure:"create_order"`
	GetOrderStatus int64         `mapstructure:"get_order_status"`
	Window         time.Duration `mapstructure:"window"`
}

type OrderGRPCRateLimitConfig struct {
	CreateOrder    int `mapstructure:"create_order"`
	GetOrderStatus int `mapstructure:"get_order_status"`
	RefreshToken   int `mapstructure:"refresh_token"`
}

type SpotGRPCRateLimitConfig struct {
	ViewMarkets   int `mapstructure:"view_markets"`
	GetMarketByID int `mapstructure:"get_market_by_id"`
}

type TracingConfig struct {
	CollectorEndpoint string `mapstructure:"exporter_otlp_endpoint"`
	Environment       string `mapstructure:"environment"`
	ServiceVersion    string `mapstructure:"service_version"`
}

type MetricsConfig struct {
	HTTPAddress     string        `mapstructure:"http_address"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

type KeepAliveConfig struct {
	PingTime            time.Duration `mapstructure:"ping_time"`
	PingTimeout         time.Duration `mapstructure:"ping_timeout"`
	MinPingInterval     time.Duration `mapstructure:"min_ping_interval"`
	PermitWithoutStream bool          `mapstructure:"permit_without_stream"`
}

type AuthVerifierConfig struct {
	JWTSecret   string   `mapstructure:"jwt_secret"`
	SkipMethods []string `mapstructure:"skip_methods"`
}

type AuthIssuerConfig struct {
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
}

type RetryConfig struct {
	MaxAttempts     uint          `mapstructure:"max_attempts"`
	InitialBackoff  time.Duration `mapstructure:"initial_backoff"`
	Jitter          float64       `mapstructure:"jitter"`
	PerRetryTimeout time.Duration `mapstructure:"per_retry_timeout"`
}

func (r RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}
