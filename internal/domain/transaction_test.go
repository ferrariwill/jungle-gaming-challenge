package domain_test

import (
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
)

func TestNewWagerTransaction_validations(t *testing.T) {
	money := domain.NewInternalMoney(2500, "BRL")

	tx, err := domain.NewWagerTransaction(
		"tx-1", "provider-a", "ext-1", "key-123", "wallet-1", "player-1", "round-1", "game-1",
		domain.KindBet, money, "",
	)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if tx.Status() != domain.StatusPending {
		t.Fatalf("Expected status %s, got %s", domain.StatusPending, tx.Status())
	}

	_, err = domain.NewWagerTransaction(
		"tx-2", "provider-a", "ext-2", "key-124", "wallet-1", "player-1", "round-1", "game-1",
		domain.KindRefund, money, "",
	)

	if err != domain.ErrMissingReference {
		t.Fatalf("Expected error %v, got %v", domain.ErrMissingReference, err)
	}
}

func TestWagerTransaction_TerminalStateProtection(t *testing.T) {
	money := domain.NewInternalMoney(2500, "BRL")
	tx, _ := domain.NewWagerTransaction(
		"tx-1", "provider-a", "ext-1", "key-123", "wallet-1", "player-1", "round-1", "game-1",
		domain.KindBet, money, "",
	)

	if err := tx.TransitionToProcessed(); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	err := tx.TransitionToPendingReference()
	if err != domain.ErrTerminalStateTransition {
		t.Fatalf("Expected error %v, got %v", domain.ErrTerminalStateTransition, err)
	}

	err = tx.TransitionToRejected("INSUFFICIENT_FUNDS")
	if err != domain.ErrTerminalStateTransition {
		t.Fatalf("Expected error %v, got %v", domain.ErrTerminalStateTransition, err)
	}
}

func TestWagerTransaction_DeterministicHash(t *testing.T) {
	money := domain.NewInternalMoney(5000, "BRL")
	tx, _ := domain.NewWagerTransaction(
		"tx-1", "provider-a", "ext-1", "key-123", "wallet-1", "player-1", "round-1", "game-1",
		domain.KindBet, money, "",
	)

	err := tx.CalculateAndSetPayloadHash()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	hash1 := tx.PayloadHash()
	if hash1 == "" {
		t.Fatalf("Expected hash, got empty string")
	}

	//Forcar o calculo do hash novamente
	_ = tx.CalculateAndSetPayloadHash()

	if hash1 != tx.PayloadHash() {
		t.Fatalf("Expected hash to be the same, got %s and %s", hash1, tx.PayloadHash())
	}
}

func TestNewWagerTransaction_LossRequiresZero(t *testing.T) {
	zero := domain.NewInternalMoney(0, "BRL")
	tx, err := domain.NewWagerTransaction(
		"tx-loss", "provider-a", "ext-loss", "key-loss", "wallet-1", "player-1", "round-1", "game-1",
		domain.KindLoss, zero, "",
	)
	if err != nil {
		t.Fatalf("expected LOSS with zero amount to be valid: %v", err)
	}
	if err := tx.TransitionToPendingReference(); err != nil {
		t.Fatalf("PENDING -> PENDING_REFERENCE must be allowed: %v", err)
	}

	nonzero := domain.NewInternalMoney(100, "BRL")
	_, err = domain.NewWagerTransaction(
		"tx-loss-2", "provider-a", "ext-loss-2", "key-loss-2", "wallet-1", "player-1", "round-1", "game-1",
		domain.KindLoss, nonzero, "",
	)
	if err != domain.ErrLossTypeMustHaveZeroValue {
		t.Fatalf("expected ErrLossTypeMustHaveZeroValue, got %v", err)
	}
}
