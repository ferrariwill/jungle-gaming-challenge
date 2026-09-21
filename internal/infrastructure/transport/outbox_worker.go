package transport

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/jackc/pgx/v5"
)

type OutboxWorker struct {
	cfg         *config.Config
	sqsClient   *sqs.Client
	messageRepo *repository.MessagingRepository
	tm          *database.TransactionManager
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

func NewOutboxWorker(
	cfg *config.Config,
	messageRepo *repository.MessagingRepository,
	tm *database.TransactionManager,
) (*OutboxWorker, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	sqsClient := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.SQSEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.SQSEndpoint)
		}
	})

	return &OutboxWorker{
		cfg:         cfg,
		sqsClient:   sqsClient,
		messageRepo: messageRepo,
		tm:          tm,
		stopChan:    make(chan struct{}),
	}, nil
}

func (w *OutboxWorker) Start() {
	w.wg.Add(1)
	defer w.wg.Done()

	log.Printf("Starting outbox worker for queue")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			log.Printf("Stopping outbox worker")
			return
		case <-ticker.C:
			w.processPendingEvents()
		}
	}
}

func (w *OutboxWorker) Stop(ctx context.Context) error {
	close(w.stopChan)

	c := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		log.Printf("Outbox worker stopped")
		return nil
	case <-ctx.Done():
		log.Printf("Outbox worker timed out")
		return ctx.Err()
	}
}

func (w *OutboxWorker) processPendingEvents() {
	ctx := context.Background()

	err := w.tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
		events, err := w.messageRepo.FindPendingOutboxEvents(ctx, tx, 10)
		if err != nil {
			return err
		}

		for _, event := range events {
			_, err := w.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
				QueueUrl:    aws.String(w.cfg.WagerQueueURL),
				MessageBody: aws.String(event.Payload),
			})

			if err != nil {
				log.Printf("Error sending message to SQS: %v", err)
				nextTry := time.Now().Add(5 * time.Second)
				_ = w.messageRepo.UpdateOutboxRetry(ctx, tx, event.ID, nextTry)
				continue
			}

			err = w.messageRepo.MarkOutboxAsPublished(ctx, tx, event.ID)
			if err != nil {
				return err
			}
			log.Printf("Published event: %s", event.ID)
		}
		return nil
	})

	if err != nil {
		log.Printf("Error processing pending events: %v", err)
	}
}
