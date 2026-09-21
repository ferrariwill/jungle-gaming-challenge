package transport

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

type PendingReferenceWorker struct {
	txRepo       *repository.TransactionRepository
	wagerUsecase *usecase.WagerUsecase
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

func NewPendingReferenceWorker(
	txRepo *repository.TransactionRepository,
	wagerUsecase *usecase.WagerUsecase,
) *PendingReferenceWorker {
	return &PendingReferenceWorker{
		txRepo:       txRepo,
		wagerUsecase: wagerUsecase,
		stopChan:     make(chan struct{}),
	}
}

func (w *PendingReferenceWorker) Start() {
	w.wg.Add(1)
	defer w.wg.Done()

	log.Printf("Starting pending reference worker")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			log.Printf("Stopping pending reference worker")
			return
		case <-ticker.C:
			w.processBatch()
		}
	}
}

func (w *PendingReferenceWorker) Stop(ctx context.Context) error {
	close(w.stopChan)
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *PendingReferenceWorker) processBatch() {
	ctx := context.Background()
	items, err := w.txRepo.FindPendingReferences(ctx, 20)
	if err != nil {
		log.Printf("pending reference batch error: %v", err)
		return
	}
	for _, item := range items {
		if err := w.wagerUsecase.ResolvePendingReference(ctx, item.ID()); err != nil {
			log.Printf("pending reference resolve failed for %s: %v", item.ID(), err)
		}
	}
}
