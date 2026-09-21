package transport_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/transport"
	"github.com/golang-jwt/jwt/v5"
)

func TestNewAuthMiddleware_Initialization(t *testing.T) {
	cfg := &config.Config{}
	mw, err := transport.NewAuthMiddleware(cfg)
	if err != nil {
		t.Fatalf("should not return error when instantiating AuthMiddleware: %v", err)
	}
	if mw == nil {
		t.Fatal("expected a valid instance of AuthMiddleware, got nil")
	}
}

func TestAuthMiddleware_BypassesHealthChecks(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})

	handlerPassed := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerPassed = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/health/live", nil)
	rec := httptest.NewRecorder()

	mw.Authenticate(nextHandler).ServeHTTP(rec, req)

	if !handlerPassed {
		t.Error("the middleware should have allowed the direct passage for the public health check route")
	}
}

func TestAuthMiddleware_RejectsMissingHeader(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("POST", "/wallets", nil)
	rec := httptest.NewRecorder()

	mw.Authenticate(nextHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized for missing header, got: %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsInvalidFormat(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("POST", "/wallets", nil)
	req.Header.Set("Authorization", "Invalid format token123")
	rec := httptest.NewRecorder()

	mw.Authenticate(nextHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid authorization format, got: %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsStaticLocalToken(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{
		IDPIssuerURL: "http://localhost:8080/realms/jungle",
	})

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/wallets", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token-provedor-a")
	rec := httptest.NewRecorder()

	mw.Authenticate(nextHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("static tokens must be rejected after OIDC enforcement, got status: %d", rec.Code)
	}
}

func TestNewHTTPHandler_Initialization(t *testing.T) {
	handler := transport.NewHTTPHandler(nil, nil, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected a valid instance of HTTPHandler, got nil")
	}
}

func TestHTTPHandler_HealthCheckEndpoints(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})
	handler := transport.NewHTTPHandler(nil, nil, nil, nil, mw, nil)

	reqLive := httptest.NewRequest("GET", "/health/live", nil)
	recLive := httptest.NewRecorder()
	handler.ServeHTTP(recLive, reqLive)

	if recLive.Code != http.StatusOK {
		t.Errorf("liveness endpoint failed with status: %d", recLive.Code)
	}

	reqReady := httptest.NewRequest("GET", "/health/ready", nil)
	recReady := httptest.NewRecorder()
	handler.ServeHTTP(recReady, reqReady)

	if recReady.Code != http.StatusOK {
		t.Errorf("readiness endpoint failed with status: %d", recReady.Code)
	}
}

func TestHTTPHandler_WageringRejectsUnauthorized(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})
	handler := transport.NewHTTPHandler(nil, nil, nil, nil, mw, nil)

	req := httptest.NewRequest("POST", "/wagering/transactions", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid JWT, got: %d", rec.Code)
	}
}

func TestSQSWorker_EnvelopeParsing(t *testing.T) {
	jsonPayload := `{
		"messageId": "msg-123",
		"type": "WagerTransactionRequested",
		"data": {
			"providerId": "provider-a",
			"externalTransactionId": "tx-ext-001",
			"idempotencyKey": "key-idemp-001",
			"playerId": "player-1",
			"walletId": "wal-1",
			"kind": "BET",
			"money": { "amount": "25.00", "currency": "BRL" },
			"referenceExternalTransactionId": "tx-ref-1"
		}
	}`

	var envelope transport.EnvelopeSQS
	err := json.Unmarshal([]byte(jsonPayload), &envelope)
	if err != nil {
		t.Fatalf("failed to perform structural unmarshal of the SQS envelope: %v", err)
	}

	if envelope.MessageID != "msg-123" || envelope.Data.Kind != "BET" {
		t.Error("the mapping or mapping of JSON tags in the SQS envelope struct is inconsistent with the contract")
	}
	if envelope.Data.ReferenceExternalID != "tx-ref-1" {
		t.Error("expected referenceExternalTransactionId mapping")
	}
}

func TestSignRSAToken_Smoke(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"azp": "provider-a-service",
		"iss": "http://localhost:8080/realms/jungle",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString(key)
	if err != nil || signed == "" {
		t.Fatalf("failed to sign token: %v", err)
	}
}
