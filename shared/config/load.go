package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

func NewViper(configDir string) (*viper.Viper, error) {
	v := viper.New()

	if err := loadIntoViper(configDir, v); err != nil {
		return nil, err
	}
	return v, nil
}

func LoadKey(configDir, key string, target any) error {
	v, err := NewViper(configDir)
	if err != nil {
		return err
	}

	if err = v.UnmarshalKey(key, target); err != nil {
		return fmt.Errorf("unmarshal %s config: %w", key, err)
	}

	return nil
}

func loadIntoViper(configDir string, v *viper.Viper) error {
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

	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config file %q: %w", configPath, err)
	}

	return nil
}
