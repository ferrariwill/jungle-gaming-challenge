package domain_test

import (
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
)

func TestNewWallet_InitialState(t *testing.T) {
	initial := domain.NewInternalMoney(1000, "BRL")
	w, err := domain.NewWallet("123", "player1", initial)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if w.Version() != 1 {
		t.Fatalf("Expected Version 1, got %d", w.Version())
	}

	if w.Balance().Amount() != 1000 {
		t.Fatalf("Expected Balance 1000, got %d", w.Balance().Amount())
	}
}

func TestWallet_Debit_SuccessAndInsufficientFunds(t *testing.T) {
	initial := domain.NewInternalMoney(10000, "BRL")
	w, err := domain.NewWallet("123", "player1", initial)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	betAmount := domain.NewInternalMoney(8000, "BRL")
	errBet := w.Debit(betAmount)

	if errBet != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if w.Balance().Amount() != 2000 {
		t.Fatalf("Expected Balance 2000, got %d", w.Balance().Amount())
	}

	if w.Version() != 2 {
		t.Fatalf("Expected Version 2, got %d", w.Version())
	}

	errBet = w.Debit(betAmount)
	if errBet != domain.ErrInsufficientBalance {
		t.Fatalf("Expected ErrInsufficientBalance, got %v", errBet)
	}

}

func TestWallet_CurrencyMismatch(t *testing.T) {
	initial := domain.NewInternalMoney(10000, "BRL")
	w, err := domain.NewWallet("123", "player1", initial)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	betAmount := domain.NewInternalMoney(8000, "USD")
	errBet := w.Debit(betAmount)
	if errBet != domain.ErrWalletCurrencyMismatch {
		t.Fatalf("Expected ErrWalletCurrencyMismatch, got %v", errBet)
	}
}
