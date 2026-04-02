package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

func LoadAll(configDir string) error {
	if configDir == "" {
		configDir = "."
	}

	envPath := filepath.Join(configDir, ".env")
	if _, err := os.Stat(envPath); err == nil {
		if err = godotenv.Load(envPath); err != nil {
			return fmt.Errorf("load .env file %q: %w", envPath, err)
		}
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("load config.yaml file %q: %w", configPath, err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("read config file %q: %w", configPath, err)
	}

	return nil
}
