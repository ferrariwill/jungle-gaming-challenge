package database_test

import (
	"context"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/jackc/pgx/v5"
)

func TestTransactionManager_PropagatesPanic(t *testing.T) {
	tm := database.NewTransactionManager(nil)

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("the TransactionManager swallowed the panic; expected it to be repropagated")
		}
	}()

	ctx := context.Background()

	_ = tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
		panic("forced panic to simulate a serious business failure")
	})
}
