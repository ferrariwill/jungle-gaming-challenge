package usecase_test

import (
	"context"
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

func TestNewOpenWalletUseCase_Initialization(t *testing.T) {
	u := usecase.NewOpenWalletUseCase(nil, nil, nil, nil, nil)
	if u == nil {
		t.Fatal("expected a valid instance of OpenWalletUsecase, got nil")
	}
}

func TestOpenWalletUseCase_RejectsUnsupportedCurrency(t *testing.T) {
	cfg := &config.Config{
		SupportedCurrencies: []string{"BRL"},
	}

	u := usecase.NewOpenWalletUseCase(cfg, nil, nil, nil, nil)

	input := usecase.OpenWalletInputDTO{
		PlayerID:        "player-marcos-123",
		InitialAmount:   "100.00",
		InitialCurrency: "EUR",
	}

	_, err := u.Execute(context.Background(), input)
	if err == nil {
		t.Error("expected error when trying to open wallet with unsupported currency, got nil")
	}
}
