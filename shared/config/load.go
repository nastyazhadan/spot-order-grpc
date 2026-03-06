package config

import (
	"errors"
	"fmt"

	"github.com/spf13/viper"
)

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	viper.AutomaticEnv()

	// Order bindings
	_ = viper.BindEnv("order.address", "ORDER_ADDRESS")
	_ = viper.BindEnv("order.db_uri", "ORDER_DB_URI")
	_ = viper.BindEnv("order.spot_address", "SPOT_INSTRUMENT_DIAL_ADDRESS")
	_ = viper.BindEnv("order.create_timeout", "ORDER_CREATE_TIMEOUT")
	_ = viper.BindEnv("order.check_timeout", "ORDER_CHECK_TIMEOUT")
	_ = viper.BindEnv("order.log_level", "LOG_LEVEL")
	_ = viper.BindEnv("order.log_format", "LOG_FORMAT")
	_ = viper.BindEnv("order.gs_timeout", "GS_TIMEOUT")
	_ = viper.BindEnv("order.max_recv_msg_size", "ORDER_MAX_RECV_MSG_SIZE")
	_ = viper.BindEnv("order.grpc_rate_limit", "ORDER_GRPC_RATE_LIMIT")

	// Order circuit breaker
	_ = viper.BindEnv("order.circuit_breaker.max_requests", "CB_MAX_REQUESTS")
	_ = viper.BindEnv("order.circuit_breaker.interval", "CB_INTERVAL")
	_ = viper.BindEnv("order.circuit_breaker.timeout", "CB_TIMEOUT")
	_ = viper.BindEnv("order.circuit_breaker.max_failures", "CB_MAX_FAILURES")

	// Order postgres pool
	_ = viper.BindEnv("order.postgres_pool.max_conns", "PG_POOL_MAX_CONNS")
	_ = viper.BindEnv("order.postgres_pool.min_conns", "PG_POOL_MIN_CONNS")
	_ = viper.BindEnv("order.postgres_pool.max_conn_lifetime", "PG_POOL_MAX_CONN_LIFETIME")
	_ = viper.BindEnv("order.postgres_pool.max_conn_idle_time", "PG_POOL_MAX_CONN_IDLE_TIME")

	// Order redis
	_ = viper.BindEnv("order.redis.host", "REDIS_HOST")
	_ = viper.BindEnv("order.redis.port", "REDIS_PORT")
	_ = viper.BindEnv("order.redis.connection_timeout", "REDIS_CONNECTION_TIMEOUT")
	_ = viper.BindEnv("order.redis.pool_size", "REDIS_POOL_SIZE")
	_ = viper.BindEnv("order.redis.min_idle", "REDIS_MIN_IDLE")
	_ = viper.BindEnv("order.redis.max_idle", "REDIS_MAX_IDLE")
	_ = viper.BindEnv("order.redis.max_active_conns", "REDIS_MAX_ACTIVE_CONNS")
	_ = viper.BindEnv("order.redis.idle_timeout", "REDIS_IDLE_TIMEOUT")
	_ = viper.BindEnv("order.redis.max_conn_lifetime", "REDIS_CONN_MAX_LIFETIME")
	_ = viper.BindEnv("order.redis.spot_cache_ttl", "SPOT_REDIS_CACHE_TTL")

	// Order rate limiter
	_ = viper.BindEnv("order.rate_limiter.create_order", "RATE_LIMIT_CREATE_ORDER")
	_ = viper.BindEnv("order.rate_limiter.get_order_status", "RATE_LIMIT_GET_ORDER_STATUS")
	_ = viper.BindEnv("order.rate_limiter.window", "RATE_LIMIT_WINDOW")

	// Spot bindings
	_ = viper.BindEnv("spot.address", "SPOT_INSTRUMENT_LISTEN_ADDRESS")
	_ = viper.BindEnv("spot.db_uri", "SPOT_DB_URI")
	_ = viper.BindEnv("spot.log_level", "LOG_LEVEL")
	_ = viper.BindEnv("spot.log_format", "LOG_FORMAT")
	_ = viper.BindEnv("spot.gs_timeout", "GS_TIMEOUT")
	_ = viper.BindEnv("spot.max_recv_msg_size", "SPOT_MAX_RECV_MSG_SIZE")
	_ = viper.BindEnv("spot.grpc_rate_limit", "SPOT_GRPC_RATE_LIMIT")

	// Spot postgres pool (те же env-переменные, что и у order)
	_ = viper.BindEnv("spot.postgres_pool.max_conns", "PG_POOL_MAX_CONNS")
	_ = viper.BindEnv("spot.postgres_pool.min_conns", "PG_POOL_MIN_CONNS")
	_ = viper.BindEnv("spot.postgres_pool.max_conn_lifetime", "PG_POOL_MAX_CONN_LIFETIME")
	_ = viper.BindEnv("spot.postgres_pool.max_conn_idle_time", "PG_POOL_MAX_CONN_IDLE_TIME")

	// Spot redis
	_ = viper.BindEnv("spot.redis.host", "REDIS_HOST")
	_ = viper.BindEnv("spot.redis.port", "REDIS_PORT")
	_ = viper.BindEnv("spot.redis.connection_timeout", "REDIS_CONNECTION_TIMEOUT")
	_ = viper.BindEnv("spot.redis.pool_size", "REDIS_POOL_SIZE")
	_ = viper.BindEnv("spot.redis.min_idle", "REDIS_MIN_IDLE")
	_ = viper.BindEnv("spot.redis.max_idle", "REDIS_MAX_IDLE")
	_ = viper.BindEnv("spot.redis.max_active_conns", "REDIS_MAX_ACTIVE_CONNS")
	_ = viper.BindEnv("spot.redis.idle_timeout", "REDIS_IDLE_TIMEOUT")
	_ = viper.BindEnv("spot.redis.max_conn_lifetime", "REDIS_CONN_MAX_LIFETIME")
	_ = viper.BindEnv("spot.redis.spot_cache_ttl", "SPOT_REDIS_CACHE_TTL")

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	if viper.GetString("order.db_uri") == "" {
		return nil, errors.New("ORDER_DB_URI is required")
	}

	if viper.GetString("spot.db_uri") == "" {
		return nil, errors.New("SPOT_DB_URI is required")
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
