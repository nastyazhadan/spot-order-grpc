package config

import (
	"fmt"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Order OrderConfig
	Spot  SpotConfig
}

type OrderConfig struct {
	Address        string        `env:"ORDER_ADDRESS" env-default:":50051"`
	DBURI          string        `env:"ORDER_DB_URI" env-required:"true"`
	SpotAddress    string        `env:"SPOT_INSTRUMENT_DIAL_ADDRESS" env-default:"localhost:50052"`
	CreateTimeout  time.Duration `env:"ORDER_CREATE_TIMEOUT" env-default:"5s"`
	CheckTimeout   time.Duration `env:"ORDER_CHECK_TIMEOUT" env-default:"2s"`
	LogLevel       string        `env:"LOG_LEVEL"  env-default:"info"`
	LogFormat      string        `env:"LOG_FORMAT" env-default:"console"`
	GSTimeout      time.Duration `env:"GS_TIMEOUT" env-default:"5s"`
	CircuitBreaker CircuitBreakerConfig
	RateLimiter    RateLimiterConfig
	Redis          RedisConfig
}

type SpotConfig struct {
	Address   string        `env:"SPOT_INSTRUMENT_LISTEN_ADDRESS" env-default:":50052"`
	DBURI     string        `env:"SPOT_DB_URI" env-required:"true"`
	LogLevel  string        `env:"LOG_LEVEL"  env-default:"info"`
	LogFormat string        `env:"LOG_FORMAT" env-default:"console"`
	GSTimeout time.Duration `env:"GS_TIMEOUT" env-default:"5s"`
	Redis     RedisConfig
}

type CircuitBreakerConfig struct {
	MaxRequests uint32        `env:"CB_MAX_REQUESTS"             env-default:"3"`
	Interval    time.Duration `env:"CB_INTERVAL"                 env-default:"10s"`
	Timeout     time.Duration `env:"CB_TIMEOUT"                  env-default:"5s"`
	MaxFailures uint32        `env:"CB_MAX_FAILURES" env-default:"5"`
}

type RedisConfig struct {
	Host              string        `env:"REDIS_HOST" env-default:"localhost"`
	Port              int           `env:"REDIS_PORT" env-default:"6379"`
	ConnectionTimeout time.Duration `env:"REDIS_CONNECTION_TIMEOUT" env-default:"10s"`
	MaxIdle           int           `env:"REDIS_MAX_IDLE" env-default:"10"`
	IdleTimeout       time.Duration `env:"REDIS_IDLE_TIMEOUT" env-default:"10s"`
	CacheTTL          time.Duration `env:"SPOT_REDIS_CACHE_TTL" env-default:"24h"`
}

type RateLimiterConfig struct {
	CreateOrder    int64         `env:"RATE_LIMIT_CREATE_ORDER" env-default:"5"`
	GetOrderStatus int64         `env:"RATE_LIMIT_GET_ORDER_STATUS" env-default:"50"`
	Window         time.Duration `env:"RATE_LIMIT_WINDOW" env-default:"1h"`
}

func (r RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

func Load(path string) (*Config, error) {
	config := &Config{}
	if err := cleanenv.ReadConfig(path, config); err != nil {
		return nil, err
	}
	return config, nil
}
