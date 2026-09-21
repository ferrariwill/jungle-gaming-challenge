package repository_test

import (
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
)

func TestRepositories_Initialization(t *testing.T) {
	walletRepo := repository.NewWalletRepository(nil)
	if walletRepo == nil {
		t.Error("failed to instantiate the WalletRepository")
	}

	txRepo := repository.NewTransactionRepository(nil)
	if txRepo == nil {
		t.Error("failed to instantiate the TransactionRepository")
	}

	messagingRepo := repository.NewMessagingRepository(nil)
	if messagingRepo == nil {
		t.Error("failed to instantiate the MessagingRepository")
	}
}
