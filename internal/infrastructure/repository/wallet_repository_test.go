package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
)

func TestNewWalletRepository_Initialization(t *testing.T) {
	repo := repository.NewWalletRepository(nil)
	if repo == nil {
		t.Fatal("expected a valid instance of WalletRepository, got nil")
	}
}

func TestWalletRepository_MethodsErrorWithNilPool(t *testing.T) {
	repo := repository.NewWalletRepository(nil)
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "wal_123")
	if !errors.Is(err, repository.ErrNilDatabasePool) {
		t.Error("expected stable ErrNilDatabasePool error when finding ID with nil pool")
	}

	_, _, err = repo.CalculateLedgerBalance(ctx, "wal_123")
	if !errors.Is(err, repository.ErrNilDatabasePool) {
		t.Error("expected stable ErrNilDatabasePool error when calculating ledger balance with nil pool")
	}

	err = repo.Save(ctx, nil, nil)
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error when saving with nil transaction")
	}

	err = repo.Update(ctx, nil, nil, 0)
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error when updating with nil transaction")
	}

	_, err = repo.FindByIDWithLock(ctx, nil, "wal_123")
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error when finding with lock with nil transaction")
	}
}
