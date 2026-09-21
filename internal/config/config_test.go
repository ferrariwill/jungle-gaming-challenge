package config_test

import (
	"os"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
)

func TestNewConfig_FallbacksAndReading(t *testing.T) {
	os.Setenv("SUPPORTED_CURRENCIES", "USD,EUR")
	defer os.Clearenv()

	cfg := config.NewConfig()

	if len(cfg.SupportedCurrencies) != 2 || cfg.SupportedCurrencies[0] != "USD" {
		t.Errorf("failed to read supported currencies from env")
	}

}
