package usecase_test

import (
	"context"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestWagerUsecase_RejectsIncompatibleCurrencies(t *testing.T) {
	cfg := &config.Config{
		SupportedCurrencies: []string{"BRL"},
	}

	u := usecase.NewWagerUsecase(cfg, nil, nil, nil, nil)

	input := usecase.InputTransactionDTO{
		Amount:   "50.00",
		Currency: "USD",
	}

	_, err := u.ProcessTransaction(context.Background(), input)
	if err == nil {
		t.Error("the usecase should have blocked the operation immediately due to the invalid currency")
	}
}
