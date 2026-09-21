package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	SupportedCurrencies []string
	DBConnectionString  string
	AWSRegion           string
	SQSEndpoint         string
	WagerQueueURL       string
	EventsQueueURL      string
	IDPIssuerURL        string
	IDPJWKSURL          string
	OutboxMaxAttempts   int
	PendingRefMaxRetry  int
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
		EventsQueueURL:      getEnv("EVENTS_QUEUE_URL", "http://localhost:4566/000000000000/wager-events"),
		IDPIssuerURL:        getEnv("IDP_ISSUER_URL", "http://localhost:8080/realms/jungle"),
		IDPJWKSURL:          getEnv("IDP_JWKS_URL", "http://localhost:8080/realms/jungle/protocol/openid-connect/certs"),
		OutboxMaxAttempts:   getEnvInt("OUTBOX_MAX_ATTEMPTS", 10),
		PendingRefMaxRetry:  getEnvInt("PENDING_REF_MAX_RETRY", 20),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	raw := getEnv(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func (c *Config) IsCurrencySupported(currency string) bool {
	for _, supported := range c.SupportedCurrencies {
		if strings.ToUpper(strings.TrimSpace(currency)) == supported {
			return true
		}
	}
	return false
}
