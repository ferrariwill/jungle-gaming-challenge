package database_test

import (
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
)

func TestNewTransactionManager_Initialization(t *testing.T) {
	tm := database.NewTransactionManager(nil)
	if tm == nil {
		t.Fatal("expected the TransactionManager to not return nil")
	}
}
