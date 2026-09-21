//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestE2E_PostgresKeycloakLocalStack(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	token := fetchKeycloakToken(t)
	handler := s.newHTTPHandler(t)

	player := uniquePlayer("e2e")
	walletBody := fmt.Sprintf(`{"playerId":"%s","initialBalance":{"amount":"100.00","currency":"BRL"}}`, player)
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBufferString(walletBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create wallet: %d %s", rec.Code, rec.Body.String())
	}
	var walletResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &walletResp); err != nil || walletResp.ID == "" {
		t.Fatalf("wallet response: %s", rec.Body.String())
	}

	extID := "e2e-bet-" + player
	betBody := fmt.Sprintf(`{
		"providerId":"provider-a",
		"externalTransactionId":"%s",
		"playerId":"%s",
		"walletId":"%s",
		"roundId":"r1",
		"gameId":"g1",
		"kind":"BET",
		"money":{"amount":"25.00","currency":"BRL"}
	}`, extID, player, walletResp.ID)

	req = httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(betBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "provider-a:"+extID)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bet: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(betBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "provider-a:"+extID)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache-Lookup") != "HIT - Idempotent Replay" {
		t.Fatalf("expected idempotent header, got %q", rec.Header().Get("X-Cache-Lookup"))
	}

	conflictBody := fmt.Sprintf(`{
		"providerId":"provider-a",
		"externalTransactionId":"%s",
		"playerId":"%s",
		"walletId":"%s",
		"roundId":"r1",
		"gameId":"g1",
		"kind":"BET",
		"money":{"amount":"10.00","currency":"BRL"}
	}`, extID, player, walletResp.ID)
	req = httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(conflictBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "provider-a:"+extID)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", rec.Code, rec.Body.String())
	}

	// Prove LocalStack queue is reachable (send), then process via the same usecase path used by the SQS worker.
	winExt := "e2e-win-" + player
	if err := s.sendWinToLocalStack(t, player, walletResp.ID, winExt); err != nil {
		t.Fatalf("localstack send: %v", err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(winExt)))
	_, err := s.wagerUC.ProcessTransactionWithInbox(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: winExt,
		IdempotencyKey:        "provider-a:" + winExt,
		PlayerID:              player,
		WalletID:              walletResp.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindWin),
		Amount:                "12.00",
		Currency:              "BRL",
	}, &usecase.InboxClaim{
		ConsumerName: "sqs_wager_consumer",
		MessageID:    "e2e-inbox-" + winExt,
		PayloadHash:  hash,
	})
	if err != nil {
		t.Fatalf("win via inbox path: %v", err)
	}

	w, err := s.walletRepo.FindByID(ctx, walletResp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().Amount() != 8700 {
		t.Fatalf("expected 87.00 (100-25+12), got %s", w.Balance().String())
	}
}
