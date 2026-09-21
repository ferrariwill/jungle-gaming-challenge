//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestSQSCrashRedeliveryAfterCommit(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	player := uniquePlayer("sqs-crash")
	wallet := s.openWallet(t, player, "100.00")

	msgID := "sqs-msg-" + player
	body := fmt.Sprintf(`{"provider":"%s"}`, player)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))

	input := usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: "sqs-crash-tx-" + player,
		IdempotencyKey:        "provider-a:sqs-crash-tx-" + player,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindWin),
		Amount:                "15.00",
		Currency:              "BRL",
	}
	inbox := &usecase.InboxClaim{
		ConsumerName: "sqs_wager_consumer",
		MessageID:    msgID,
		PayloadHash:  hash,
	}

	// First delivery: commit wallet+inbox+outbox (simulates success before DeleteMessage).
	out1, err := s.wagerUC.ProcessTransactionWithInbox(ctx, input, inbox)
	if err != nil {
		t.Fatal(err)
	}
	if out1.IdempotentReplay || out1.Status != string(domain.StatusProcessed) {
		t.Fatalf("first delivery unexpected: %+v", out1)
	}
	if s.countInbox(t, msgID) != 1 {
		t.Fatal("inbox not persisted after commit")
	}

	balAfterCommit, err := s.walletRepo.FindByID(ctx, wallet.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Crash before DeleteMessage → SQS redelivers same messageId.
	out2, err := s.wagerUC.ProcessTransactionWithInbox(ctx, input, inbox)
	if err != nil {
		t.Fatal(err)
	}
	if !out2.IdempotentReplay {
		t.Fatalf("redelivery must be idempotent replay, got %+v", out2)
	}

	balAfterRedelivery, err := s.walletRepo.FindByID(ctx, wallet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balAfterRedelivery.Balance().Amount() != balAfterCommit.Balance().Amount() {
		t.Fatalf("balance changed on redelivery: %s -> %s",
			balAfterCommit.Balance().String(), balAfterRedelivery.Balance().String())
	}
	if s.countInbox(t, msgID) != 1 {
		t.Fatal("inbox must stay single-row on redelivery")
	}
	if s.countTxByExternal(t, "provider-a", input.ExternalTransactionID) != 1 {
		t.Fatal("duplicate transaction on redelivery")
	}
}

func TestSQSSendAndProcessViaLocalStack(t *testing.T) {
	s := newTestStack(t)
	ctx := context.Background()
	player := uniquePlayer("sqs-ls")
	wallet := s.openWallet(t, player, "100.00")

	extID := "sqs-ls-" + player
	body := fmt.Sprintf(`{
		"messageId": "msg-%s",
		"type": "WagerTransactionRequested",
		"occurredAt": "%s",
		"data": {
			"providerId": "provider-a",
			"externalTransactionId": "%s",
			"idempotencyKey": "provider-a:%s",
			"playerId": "%s",
			"walletId": "%s",
			"roundId": "r1",
			"gameId": "g1",
			"kind": "WIN",
			"money": {"amount": "7.00", "currency": "BRL"}
		}
	}`, player, time.Now().UTC().Format(time.RFC3339), extID, extID, player, wallet.ID)

	_, err := s.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(s.cfg.WagerQueueURL),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String(player),
		MessageDeduplicationId: aws.String(extID),
	})
	if err != nil {
		t.Fatalf("localstack send: %v (is LocalStack up with FIFO queue?)", err)
	}

	recv, err := s.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(s.cfg.WagerQueueURL),
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     5,
		VisibilityTimeout:   30,
	})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(recv.Messages) == 0 {
		t.Fatal("no message received from LocalStack")
	}
	msg := recv.Messages[0]
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(*msg.Body)))

	out, err := s.wagerUC.ProcessTransactionWithInbox(ctx, usecase.InputTransactionDTO{
		ProviderID:            "provider-a",
		ExternalTransactionID: extID,
		IdempotencyKey:        "provider-a:" + extID,
		PlayerID:              player,
		WalletID:              wallet.ID,
		RoundID:               "r1",
		GameID:                "g1",
		Kind:                  string(domain.KindWin),
		Amount:                "7.00",
		Currency:              "BRL",
	}, &usecase.InboxClaim{
		ConsumerName: "sqs_wager_consumer",
		MessageID:    *msg.MessageId,
		PayloadHash:  hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != string(domain.StatusProcessed) {
		t.Fatalf("expected PROCESSED, got %s", out.Status)
	}

	_, _ = s.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.cfg.WagerQueueURL),
		ReceiptHandle: msg.ReceiptHandle,
	})
}
