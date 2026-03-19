package config

import (
	"fmt"
	"time"
)

type OrderConfig struct {
	Address         string                   `mapstructure:"address"`
	ServiceName     string                   `mapstructure:"service_name"`
	DBURI           string                   `mapstructure:"db_uri"`
	SpotAddress     string                   `mapstructure:"spot_address"`
	JWTSecret       string                   `mapstructure:"jwt_secret"`
	ServiceTimeout  time.Duration            `mapstructure:"service_timeout"`
	CheckTimeout    time.Duration            `mapstructure:"check_timeout"`
	LogLevel        string                   `mapstructure:"log_level"`
	LogFormat       string                   `mapstructure:"log_format"`
	MaxRecvMsgSize  int                      `mapstructure:"max_recv_msg_size"`
	Auth            AuthConfig               `mapstructure:"auth"`
	CircuitBreaker  CircuitBreakerConfig     `mapstructure:"circuit_breaker"`
	PostgresPool    PostgresPoolConfig       `mapstructure:"postgres_pool"`
	GRPCRateLimit   OrderGRPCRateLimitConfig `mapstructure:"grpc_rate_limit"`
	RateLimitByUser RateLimiterByUserConfig  `mapstructure:"rate_limit_by_user"`
	Redis           RedisConfig              `mapstructure:"redis"`
	Tracing         TracingConfig            `mapstructure:"tracing"`
	Metrics         MetricsConfig            `mapstructure:"metrics"`
	KeepAlive       KeepAliveConfig          `mapstructure:"keep_alive"`
}

type SpotConfig struct {
	Address        string                  `mapstructure:"address"`
	ServiceName    string                  `mapstructure:"service_name"`
	DBURI          string                  `mapstructure:"db_uri"`
	LogLevel       string                  `mapstructure:"log_level"`
	LogFormat      string                  `mapstructure:"log_format"`
	ServiceTimeout time.Duration           `mapstructure:"service_timeout"`
	MaxRecvMsgSize int                     `mapstructure:"max_recv_msg_size"`
	PostgresPool   PostgresPoolConfig      `mapstructure:"postgres_pool"`
	GRPCRateLimit  SpotGRPCRateLimitConfig `mapstructure:"grpc_rate_limit"`
	Redis          RedisConfig             `mapstructure:"redis"`
	Tracing        TracingConfig           `mapstructure:"tracing"`
	Metrics        MetricsConfig           `mapstructure:"metrics"`
	KeepAlive      KeepAliveConfig         `mapstructure:"keep_alive"`
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

type RateLimiterByUserConfig struct {
	CreateOrder    int64         `mapstructure:"create_order"`
	GetOrderStatus int64         `mapstructure:"get_order_status"`
	Window         time.Duration `mapstructure:"window"`
}

type OrderGRPCRateLimitConfig struct {
	CreateOrder    int `mapstructure:"create_order"`
	GetOrderStatus int `mapstructure:"get_order_status"`
}

type SpotGRPCRateLimitConfig struct {
	ViewMarkets int `mapstructure:"view_markets"`
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
	PushGatewayURL    string        `mapstructure:"push_gateway_url"`
}

type KeepAliveConfig struct {
	PingTime            time.Duration `mapstructure:"ping_time"`
	PingTimeout         time.Duration `mapstructure:"ping_timeout"`
	MinPingInterval     time.Duration `mapstructure:"min_ping_interval"`
	PermitWithoutStream bool          `mapstructure:"permit_without_stream"`
}

type AuthConfig struct {
	SkipMethods     []string      `mapstructure:"skip_methods"`
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
}

func (r RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}
