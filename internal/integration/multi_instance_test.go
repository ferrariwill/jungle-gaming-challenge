//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
	"github.com/jackc/pgx/v5"
)

func TestThreeIndependentInstancesConcurrentBets(t *testing.T) {
	base := newTestStack(t)
	ctx := context.Background()

	instances := []*testStack{
		base,
		base.newIndependentInstance(t),
		base.newIndependentInstance(t),
	}

	player := uniquePlayer("multi3")
	wallet := base.openWallet(t, player, "100.00")

	var wg sync.WaitGroup
	results := make([]string, 3)
	for i, inst := range instances {
		wg.Add(1)
		go func(i int, uc *usecase.WagerUsecase) {
			defer wg.Done()
			out, err := uc.ProcessTransaction(ctx, usecase.InputTransactionDTO{
				ProviderID:            "provider-a",
				ExternalTransactionID: fmt.Sprintf("multi3-%s-%d", player, i),
				IdempotencyKey:        fmt.Sprintf("provider-a:multi3-%s-%d", player, i),
				PlayerID:              player,
				WalletID:              wallet.ID,
				RoundID:               "r1",
				GameID:                "g1",
				Kind:                  string(domain.KindBet),
				Amount:                "40.00",
				Currency:              "BRL",
			})
			if err != nil {
				results[i] = "ERR:" + err.Error()
				return
			}
			results[i] = out.Status
		}(i, inst.wagerUC)
	}
	wg.Wait()

	processed := 0
	for _, st := range results {
		if st == string(domain.StatusProcessed) {
			processed++
		}
	}
	if processed != 2 {
		t.Fatalf("expected exactly 2 PROCESSED among 3 instances, got %d statuses=%v", processed, results)
	}

	w, err := base.walletRepo.FindByID(ctx, wallet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().Amount() != 2000 {
		t.Fatalf("expected 20.00 left, got %s (statuses=%v)", w.Balance().String(), results)
	}
	if w.Balance().IsNegative() {
		t.Fatal("negative balance")
	}
}

func TestTwoConcurrentOutboxPublishersSkipLocked(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	player := uniquePlayer("outbox2")
	wallet := s.openWallet(t, player, "50.00")

	// Generate several pending outbox events via wallet+bet
	_, err := s.wagerUC.ProcessTransaction(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: "outbox-seed-" + player,
		IdempotencyKey:        "provider-a:outbox-seed-" + player,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindBet),
		Amount:                "5.00",
		Currency:              "BRL",
	})
	if err != nil {
		t.Fatal(err)
	}

	pendingBefore := s.countOutboxByStatus(t, "PENDING")
	if pendingBefore < 1 {
		t.Fatal("expected pending outbox events")
	}

	claim := func(tm *database.TransactionManager, repo *repository.MessagingRepository) []string {
		var ids []string
		_ = tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
			events, err := repo.FindPendingOutboxEvents(ctx, tx, 50)
			if err != nil {
				return err
			}
			for _, e := range events {
				ids = append(ids, e.ID)
				// Hold the locks briefly so the sibling publisher must SKIP LOCKED
				if err := repo.MarkOutboxAsPublished(ctx, tx, e.ID); err != nil {
					return err
				}
			}
			return nil
		})
		return ids
	}

	instA := s
	instB := s.newIndependentInstance(t)

	var wg sync.WaitGroup
	var aIDs, bIDs []string
	wg.Add(2)
	go func() {
		defer wg.Done()
		aIDs = claim(instA.tm, instA.messageRepo)
	}()
	go func() {
		defer wg.Done()
		bIDs = claim(instB.tm, instB.messageRepo)
	}()
	wg.Wait()

	seen := map[string]int{}
	for _, id := range append(aIDs, bIDs...) {
		seen[id]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("outbox event %s claimed by both publishers", id)
		}
	}
	if len(aIDs)+len(bIDs) == 0 {
		t.Fatal("no events claimed")
	}
	if s.countOutboxByStatus(t, "PENDING") != 0 {
		t.Fatalf("expected all claimed events published, pending=%d", s.countOutboxByStatus(t, "PENDING"))
	}
}
