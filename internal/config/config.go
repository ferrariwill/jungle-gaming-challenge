package config

import (
	"os"
	"strings"
)

type Config struct {
	SupportedCurrencies []string
}

func NewConfig() *Config {
	currenciesStr := os.Getenv("SUPPORTED_CURRENCIES")

	if currenciesStr == "" {
		currenciesStr = "BRL" //fallback default
	}

	currencies := strings.Split(currenciesStr, ",")
	for i, c := range currencies {
		currencies[i] = strings.TrimSpace(strings.ToUpper(c))
	}

	return &Config{
		SupportedCurrencies: currencies,
	}
}

func (c *Config) IsCurrencySupported(currency string) bool {
	for _, c := range c.SupportedCurrencies {
		if strings.ToUpper(strings.TrimSpace(currency)) == c {
			return true
		}
	}
	return false
}
