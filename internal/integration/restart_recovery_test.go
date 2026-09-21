//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestRestartPreservesIdempotencyPendingAndOutbox(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	player := uniquePlayer("restart")
	wallet := s.openWallet(t, player, "100.00")

	betExt := "restart-bet-" + player
	betOut, err := s.wagerUC.ProcessTransaction(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: betExt,
		IdempotencyKey:        "provider-a:" + betExt,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindBet),
		Amount:                "10.00",
		Currency:              "BRL",
	})
	if err != nil {
		t.Fatal(err)
	}

	refundExt := "restart-refund-" + player
	missingRef := "missing-ref-" + player
	refundOut, err := s.wagerUC.ProcessTransaction(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: refundExt,
		IdempotencyKey:        "provider-a:" + refundExt,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindRefund),
		Amount:                "10.00",
		Currency:              "BRL",
		ReferenceExternalID:   missingRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refundOut.Status != string(domain.StatusPendingReference) {
		t.Fatalf("expected PENDING_REFERENCE, got %s", refundOut.Status)
	}

	pendingOutbox := s.countOutboxByStatus(t, "PENDING")
	if pendingOutbox < 1 {
		t.Fatal("expected pending outbox before restart")
	}

	var refundID string
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM wager_transactions WHERE external_transaction_id = $1`, refundExt).Scan(&refundID); err != nil {
		t.Fatal(err)
	}

	// Simulate crash + restart with a fresh process graph on the same database.
	time.Sleep(10 * time.Millisecond)
	s2 := s.newIndependentInstance(t)

	replay, err := s2.wagerUC.ProcessTransaction(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: betExt,
		IdempotencyKey:        "provider-a:" + betExt,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindBet),
		Amount:                "10.00",
		Currency:              "BRL",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay || replay.TransactionID != betOut.TransactionID {
		t.Fatalf("idempotency lost after restart: %+v", replay)
	}

	// Create the missing reference, then resolve pending refund via the restarted instance.
	_, err = s2.wagerUC.ProcessTransaction(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: missingRef,
		IdempotencyKey:        "provider-a:" + missingRef,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindWin),
		Amount:                "1.00",
		Currency:              "BRL",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.wagerUC.ResolvePendingReference(ctx, refundID); err != nil {
		t.Fatalf("resolve pending after restart: %v", err)
	}

	var refundStatus string
	if err := s2.pool.QueryRow(ctx,
		`SELECT status FROM wager_transactions WHERE id = $1`, refundID).Scan(&refundStatus); err != nil {
		t.Fatal(err)
	}
	if refundStatus != string(domain.StatusProcessed) {
		t.Fatalf("pending refund not recovered after restart, status=%s", refundStatus)
	}

	after := s2.countOutboxByStatus(t, "PENDING")
	if after < 1 {
		t.Fatalf("outbox pending should survive restart, got %d (before=%d)", after, pendingOutbox)
	}

	w, err := s2.walletRepo.FindByID(ctx, wallet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().IsNegative() {
		t.Fatal("negative balance after restart recovery")
	}
}
