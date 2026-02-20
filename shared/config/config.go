package config

import (
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
	SpotAddress    string        `env:"SPOT_INSTRUMENT_ADDRESS" env-default:":50052"`
	CreateTimeout  time.Duration `env:"ORDER_CREATE_TIMEOUT" env-default:"5s"`
	CheckTimeout   time.Duration `env:"ORDER_CHECK_TIMEOUT" env-default:"2s"`
	LogLevel       string        `env:"LOG_LEVEL"  env-default:"info"`
	LogFormat      string        `env:"LOG_FORMAT" env-default:"console"`
	GSTimeout      time.Duration `env:"GS_TIMEOUT" env-default:"5s"`
	CircuitBreaker CircuitBreakerConfig
}

type SpotConfig struct {
	Address   string        `env:"SPOT_INSTRUMENT_ADDRESS" env-default:":50052"`
	DBURI     string        `env:"SPOT_DB_URI" env-required:"true"`
	LogLevel  string        `env:"LOG_LEVEL"  env-default:"info"`
	LogFormat string        `env:"LOG_FORMAT" env-default:"console"`
	GSTimeout time.Duration `env:"GS_TIMEOUT" env-default:"5s"`
}

type CircuitBreakerConfig struct {
	MaxRequests uint32        `env:"CB_MAX_REQUESTS"             env-default:"3"`
	Interval    time.Duration `env:"CB_INTERVAL"                 env-default:"10s"`
	Timeout     time.Duration `env:"CB_TIMEOUT"                  env-default:"5s"`
	MaxFailures uint32        `env:"CB_MAX_FAILURES" env-default:"5"`
}

func Load(path string) (*Config, error) {
	config := &Config{}
	if err := cleanenv.ReadConfig(path, config); err != nil {
		return nil, err
	}
	return config, nil
}
