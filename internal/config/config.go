package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	SupportedCurrencies []string
	DBConnectionString  string
	AWSRegion           string
	SQSEndpoint         string
	WagerQueueURL       string
}

func NewConfig() *Config {
	currenciesStr := getEnv("SUPPORTED_CURRENCIES", "BRL")

	currencies := strings.Split(currenciesStr, ",")
	for i, c := range currencies {
		currencies[i] = strings.TrimSpace(strings.ToUpper(c))
	}

	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "jungle_user")
	dbPassword := getEnv("DB_PASSWORD", "jungle_password")
	dbName := getEnv("DB_NAME", "jungle_wagering_ledger")
	dbSSLMode := getEnv("DB_SSLMODE", "disable")

	dbURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		dbUser, dbPassword, dbHost, dbPort, dbName, dbSSLMode)

	return &Config{
		SupportedCurrencies: currencies,
		DBConnectionString:  dbURL,
		AWSRegion:           getEnv("AWS_REGION", "us-east-1"),
		SQSEndpoint:         getEnv("SQS_ENDPOINT", "http://localhost:4566"),
		WagerQueueURL:       getEnv("WAGER_QUEUE_URL", "http://localhost:4566/000000000000/wager-transactions.fifo"),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func (c *Config) IsCurrencySupported(currency string) bool {
	for _, c := range c.SupportedCurrencies {
		if strings.ToUpper(strings.TrimSpace(currency)) == c {
			return true
		}
	}
	return false
}
