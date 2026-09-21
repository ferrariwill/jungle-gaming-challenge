package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
)

func TestNewMessagingRepository_Initialization(t *testing.T) {
	repo := repository.NewMessagingRepository(nil)
	if repo == nil {
		t.Fatal("Esperava uma instância válida de MessagingRepository, obteve nil")
	}
}

func TestMessagingRepository_MethodsErrorWithNilPool(t *testing.T) {
	repo := repository.NewMessagingRepository(nil)
	ctx := context.Background()

	_, err := repo.FindPendingOutboxEvents(ctx, nil, 10)
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error when passing nil transaction, but got another result")
	}

	err = repo.MarkOutboxAsPublished(ctx, nil, "evt_123")
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error")
	}

	err = repo.UpdateOutboxRetry(ctx, nil, "evt_123", time.Now(), 10)
	if !errors.Is(err, repository.ErrNilTransaction) {
		t.Error("expected stable ErrNilTransaction error")
	}
}
