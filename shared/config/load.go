package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

func LoadAll() error {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}

	configFound := false
	for _, path := range []string{"config.yaml", "../config.yaml", "../../config.yaml"} {
		if _, err := os.Stat(path); err == nil {
			viper.SetConfigFile(path)
			configFound = true
			break
		}
	}
	if !configFound {
		return fmt.Errorf("config.yaml not found")
	}

	var notFound viper.ConfigFileNotFoundError
	if err := viper.ReadInConfig(); err != nil && !errors.As(err, &notFound) {
		return fmt.Errorf("read config.yaml file: %w", err)
	}

	return nil
}
