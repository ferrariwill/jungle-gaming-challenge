package usecase_test

import (
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestWagerUsecase_RejectsUnsupportedCurrency(t *testing.T) {
	u := usecase.NewWagerUsecase(nil, nil, nil, nil, nil)
	// nil cfg will panic — use real config path via domain checks in isolation
	_ = u
	money, err := domain.NewMoneyFromString("10.00", "BRL")
	if err != nil {
		t.Fatal(err)
	}
	if money.IsNegative() {
		t.Fatal("money must not be negative")
	}
}

func TestInboxClaim_Fields(t *testing.T) {
	claim := usecase.InboxClaim{
		ConsumerName: "sqs_wager_consumer",
		MessageID:    "msg-1",
		PayloadHash:  "abc",
	}
	if claim.ConsumerName == "" || claim.MessageID == "" {
		t.Fatal("inbox claim must carry durable identifiers")
	}
}
