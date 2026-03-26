package config

import (
	"fmt"
	"time"
)

type OrderConfig struct {
	Service         ServiceConfig            `mapstructure:"service"`
	SpotAddress     string                   `mapstructure:"spot_address"`
	Timeouts        TimeoutsConfig           `mapstructure:"timeouts"`
	Log             LogConfig                `mapstructure:"log"`
	Auth            AuthConfig               `mapstructure:"auth"`
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
	Log           LogConfig               `mapstructure:"log"`
	Timeouts      TimeoutsConfig          `mapstructure:"timeouts"`
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

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type TimeoutsConfig struct {
	Service time.Duration `mapstructure:"service"`
	Check   time.Duration `mapstructure:"check"`
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
	GroupID           string        `mapstructure:"group_id"`
	SessionTimeout    time.Duration `mapstructure:"session_timeout"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	MaxRetries        int           `mapstructure:"max_retries"`
	RetryBackoff      time.Duration `mapstructure:"retry_backoff"`
	DLQEnabled        bool          `mapstructure:"dlq_enabled"`
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
	HTTPAddress       string        `mapstructure:"http_address"`
	CollectorEndpoint string        `mapstructure:"collector_endpoint"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ExportInterval    time.Duration `mapstructure:"export_interval"`
}

type KeepAliveConfig struct {
	PingTime            time.Duration `mapstructure:"ping_time"`
	PingTimeout         time.Duration `mapstructure:"ping_timeout"`
	MinPingInterval     time.Duration `mapstructure:"min_ping_interval"`
	PermitWithoutStream bool          `mapstructure:"permit_without_stream"`
}

type AuthConfig struct {
	JWTSecret       string        `mapstructure:"jwt_secret"`
	SkipMethods     []string      `mapstructure:"skip_methods"`
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
