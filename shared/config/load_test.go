package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSuccess(t *testing.T) {
	viper.Reset()
	viper.Set("order.db_uri", "postgres://localhost/order_test")
	viper.Set("spot.db_uri", "postgres://localhost/spot_test")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("../..")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ":50051", cfg.Order.Address)
	assert.Equal(t, ":50052", cfg.Spot.Address)
	assert.Equal(t, "info", cfg.Order.LogLevel)
}

func TestMissingOrderDBURI(t *testing.T) {
	viper.Reset()

	viper.Set("spot.db_uri", "postgres://localhost/spot_test")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ORDER_DB_URI is required")
}

func TestMissingSpotDBURI(t *testing.T) {
	viper.Reset()
	viper.Set("order.db_uri", "postgres://localhost/order_test")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SPOT_DB_URI is required")
}

func TestMissingBothDBURIs(t *testing.T) {
	viper.Reset()

	_, err := Load()
	require.Error(t, err)
}

func TestEnvOverridesYaml(t *testing.T) {
	viper.Reset()
	viper.Set("order.db_uri", "postgres://localhost/order_test")
	viper.Set("spot.db_uri", "postgres://localhost/spot_test")
	viper.Set("order.address", ":9999")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ":9999", cfg.Order.Address)
}
