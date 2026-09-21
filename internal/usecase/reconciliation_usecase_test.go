package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestNewReconciliationUsecase_Initialization(t *testing.T) {
	u := usecase.NewReconciliationUsecase(nil)
	if u == nil {
		t.Fatal("expected a valid instance of ReconciliationUsecase, got nil")
	}
}

func TestReconciliationUsecase_AbortsOnMissingWallet(t *testing.T) {
	u := usecase.NewReconciliationUsecase(nil)

	_, err := u.Execute(context.Background(), "wal_id_inexistente")
	if !errors.Is(err, usecase.ErrNilWalletRepository) {
		t.Errorf("expected stable ErrNilWalletRepository error when passing nil repository, got: %v", err)
	}
}
