package transport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
	"github.com/jackc/pgx/v5"
)

type EnvelopeSQS struct {
	MessageID  string `json:"messageId"`
	Type       string `json:"type"`
	OccurredAt string `json:"occurredAt"`
	Data       struct {
		ProviderID            string `json:"providerId"`
		ExternalTransactionID string `json:"externalTransactionId"`
		IdempotencyKey        string `json:"idempotencyKey"`
		PlayerID              string `json:"playerId"`
		WalletID              string `json:"walletId"`
		RounID                string `json:"roundId"`
		GameID                string `json:"gameId"`
		Kind                  string `json:"kind"`
		Money                 struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"money"`
	} `json:"data"`
}

type SQSWorker struct {
	cfg          *config.Config
	sqsClient    *sqs.Client
	wagerUsecase *usecase.WagerUsecase
	messageRepo  *repository.MessagingRepository
	tm           *database.TransactionManager
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

func NewSQSWorker(
	cfg *config.Config,
	wagerUsecase *usecase.WagerUsecase,
	messageRepo *repository.MessagingRepository,
	tm *database.TransactionManager,
) (*SQSWorker, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.AWSRegion))

	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	sqsClient := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.SQSEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.SQSEndpoint)
		}
	})

	return &SQSWorker{
		cfg:          cfg,
		sqsClient:    sqsClient,
		wagerUsecase: wagerUsecase,
		messageRepo:  messageRepo,
		tm:           tm,
		stopChan:     make(chan struct{}),
	}, nil

}

func (w *SQSWorker) Start() {
	w.wg.Add(1)
	defer w.wg.Done()
	log.Printf("Starting SQS worker for queue")

	for {
		select {
		case <-w.stopChan:
			log.Printf("Stopping SQS worker")
			return
		default:
			output, err := w.sqsClient.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(w.cfg.WagerQueueURL),
				MaxNumberOfMessages: 1,
				VisibilityTimeout:   5,
				WaitTimeSeconds:     30,
			})

			if err != nil {
				log.Printf("Error receiving message from SQS: %v", err)
				time.Sleep(3 * time.Second)
				continue
			}

			for _, message := range output.Messages {
				w.processSQSEnvelope(message)
			}
		}
	}
}

func (w *SQSWorker) Stop(ctx context.Context) error {
	close(w.stopChan)

	c := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		log.Printf("SQS worker stopped")
		return nil
	case <-ctx.Done():
		log.Printf("SQS worker timed out")
		return ctx.Err()
	}
}

func (w *SQSWorker) processSQSEnvelope(message types.Message) {
	var envelope EnvelopeSQS
	if err := json.Unmarshal([]byte(*message.Body), &envelope); err != nil {
		log.Printf("Error unmarshalling envelope: %v", err)
		w.deleteMessage(message)
		return
	}

	hash := sha256.Sum256([]byte(*message.Body))
	payloadHash := fmt.Sprintf("%x", hash)

	ctx := context.Background()

	err := w.tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
		err := w.messageRepo.SaveInbox(ctx, tx, "sqs_wager_consumer", *message.MessageId, payloadHash)
		if err != nil {
			if errors.Is(err, repository.ErrDuplicateMessage) {
				log.Printf("Duplicate message detected: %v", err)
				return nil
			}
		}

		input := usecase.InputTransactionDTO{
			ProviderID:            envelope.Data.ProviderID,
			ExternalTransactionID: envelope.Data.ExternalTransactionID,
			IdempotencyKey:        envelope.Data.IdempotencyKey,
			PlayerID:              envelope.Data.PlayerID,
			WalletID:              envelope.Data.WalletID,
			RoundID:               envelope.Data.RounID,
			GameID:                envelope.Data.GameID,
			Kind:                  envelope.Data.Kind,
			Amount:                envelope.Data.Money.Amount,
			Currency:              envelope.Data.Money.Currency,
		}

		_, err = w.wagerUsecase.ProcessTransaction(ctx, input)
		return err
	})

	if err != nil {
		log.Printf("Error processing transaction: %v", err)
		return
	}

	w.deleteMessage(message)
}

func (w *SQSWorker) deleteMessage(message types.Message) {
	_, err := w.sqsClient.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(w.cfg.WagerQueueURL),
		ReceiptHandle: message.ReceiptHandle,
	})
	if err != nil {
		log.Printf("Error deleting message from SQS: %v", err)
	}
}
