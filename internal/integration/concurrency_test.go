//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestTwoBetsOf80OnBalance100(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	player := uniquePlayer("two80")
	wallet := s.openWallet(t, player, "100.00")

	var wg sync.WaitGroup
	results := make([]usecase.OutputTransactionDTO, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.wagerUC.ProcessTransaction(ctx, usecase.InputTransactionDTO{
				ProviderID:            "provider-a",
				ExternalTransactionID: fmt.Sprintf("bet80-%s-%d", player, i),
				IdempotencyKey:        fmt.Sprintf("provider-a:bet80-%s-%d", player, i),
				PlayerID:              player,
				WalletID:              wallet.ID,
				RoundID:               "r1",
				GameID:                "g1",
				Kind:                  string(domain.KindBet),
				Amount:                "80.00",
				Currency:              "BRL",
			})
		}(i)
	}
	wg.Wait()

	processed, rejected := 0, 0
	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("unexpected error on bet %d: %v", i, errs[i])
		}
		switch results[i].Status {
		case string(domain.StatusProcessed):
			processed++
		case string(domain.StatusRejected):
			rejected++
		default:
			t.Fatalf("unexpected status %s", results[i].Status)
		}
	}
	if processed != 1 || rejected != 1 {
		t.Fatalf("expected 1 PROCESSED and 1 REJECTED, got processed=%d rejected=%d", processed, rejected)
	}

	w, err := s.walletRepo.FindByID(ctx, wallet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().Amount() != 2000 {
		t.Fatalf("expected balance 20.00, got %s", w.Balance().String())
	}
	if s.countLedger(t, wallet.ID) != 2 { // OPENING + one BET debit
		t.Fatalf("expected 2 ledger entries (opening+bet), got %d", s.countLedger(t, wallet.ID))
	}
}

func TestFiftySimultaneousReplaysSameTransaction(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	player := uniquePlayer("replay50")
	wallet := s.openWallet(t, player, "100.00")

	input := usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-replay-50-" + player,
		IdempotencyKey:        "provider-a:tx-replay-50-" + player,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindBet),
		Amount:                "10.00",
		Currency:              "BRL",
	}

	first, err := s.wagerUC.ProcessTransaction(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != string(domain.StatusProcessed) || first.IdempotentReplay {
		t.Fatalf("first call must process once, got %+v", first)
	}

	var wg sync.WaitGroup
	var replays int64
	var failures int64
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := s.wagerUC.ProcessTransaction(ctx, input)
			if err != nil {
				atomic.AddInt64(&failures, 1)
				return
			}
			if out.IdempotentReplay && out.TransactionID == first.TransactionID {
				atomic.AddInt64(&replays, 1)
			}
		}()
	}
	wg.Wait()

	if failures != 0 {
		t.Fatalf("unexpected failures: %d", failures)
	}
	if replays != n {
		t.Fatalf("expected %d idempotent replays, got %d", n, replays)
	}
	if s.countTxByExternal(t, "provider-a", input.ExternalTransactionID) != 1 {
		t.Fatal("duplicate wager_transactions rows")
	}
	if s.countLedger(t, wallet.ID) != 2 {
		t.Fatalf("ledger must not grow on replays, got %d", s.countLedger(t, wallet.ID))
	}
	w, _ := s.walletRepo.FindByID(ctx, wallet.ID)
	if w.Balance().Amount() != 9000 {
		t.Fatalf("expected 90.00 after single 10 debit, got %s", w.Balance().String())
	}
}
