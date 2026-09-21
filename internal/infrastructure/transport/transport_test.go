package transport_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/transport"
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

func TestAuthMiddleware_ValidatesStaticLocalToken(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})

	contextVerified := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerID, ok := r.Context().Value(transport.ProviderIDKey).(string)
		if ok && providerID == "provider-a" {
			contextVerified = true
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/wallets", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token-provedor-a")
	rec := httptest.NewRecorder()

	mw.Authenticate(nextHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("should accept the static token configured for local tests, got status: %d", rec.Code)
	}
	if !contextVerified {
		t.Error("the token was validated but the corresponding providerId was not mapped in the request context")
	}
}

func TestNewHTTPHandler_Initialization(t *testing.T) {
	handler := transport.NewHTTPHandler(nil, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected a valid instance of HTTPHandler, got nil")
	}
}

func TestHTTPHandler_HealthCheckEndpoints(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})
	handler := transport.NewHTTPHandler(nil, nil, nil, nil, mw)

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

func TestHTTPHandler_WageringRejectsMissingIdempotencyKey(t *testing.T) {
	mw, _ := transport.NewAuthMiddleware(&config.Config{})
	handler := transport.NewHTTPHandler(nil, nil, nil, nil, mw)

	req := httptest.NewRequest("POST", "/wagering/transactions", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token-provedor-a")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request for missing idempotency key, got: %d", rec.Code)
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
			"money": { "amount": "25.00", "currency": "BRL" }
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
}
