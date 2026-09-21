package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
)

func TestNewTransactionRepository_Initialization(t *testing.T) {
	repo := repository.NewTransactionRepository(nil)
	if repo == nil {
		t.Fatal("expected a valid instance of TransactionRepository, got nil")
	}
}

func TestTransactionRepository_MethodsErrorWithNilPool(t *testing.T) {
	repo := repository.NewTransactionRepository(nil)
	ctx := context.Background()

	_, err := repo.FindByExternalID(ctx, "provider-a", "tx-ext-123")
	if !errors.Is(err, repository.ErrNilDatabasePool) {
		t.Error("expected stable ErrNilDatabasePool error when reading from a nil pool")
	}

	err = repo.UpdateStatus(ctx, nil, nil)
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error when updating status with nil transaction")
	}

	err = repo.SaveLedgerEntry(ctx, nil, repository.IDLEntryDTO{})
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error when saving ledger with nil transaction")
	}
}
