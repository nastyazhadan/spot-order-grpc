package config

import (
	"errors"
	"fmt"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

func LoadAll() error {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}

	viper.SetConfigFile("config.yaml")

	var notFound viper.ConfigFileNotFoundError
	if err := viper.ReadInConfig(); err != nil && !errors.As(err, &notFound) {
		return fmt.Errorf("read config file: %w", err)
	}

	return nil
}
