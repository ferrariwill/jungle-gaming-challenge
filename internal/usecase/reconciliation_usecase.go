package usecase

import (
	"context"
	"log"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
)

type ReconciliationOutputDTO struct {
	WalletID          string   `json:"walletId"`
	StoredBalance     MoneyDTO `json:"storedBalance"`
	CalculatedBalance MoneyDTO `json:"calculatedBalance"`
	Difference        MoneyDTO `json:"difference"`
	Consistent        bool     `json:"consistent"`
	CheckedEntries    int64    `json:"checkedEntries"`
}

type ReconciliationUsecase struct {
	walletRepo *repository.WalletRepository
}

func NewReconciliationUsecase(walletRepo *repository.WalletRepository) *ReconciliationUsecase {
	return &ReconciliationUsecase{
		walletRepo: walletRepo,
	}
}

func (u *ReconciliationUsecase) Execute(ctx context.Context, walletID string) (ReconciliationOutputDTO, error) {
	wallet, err := u.walletRepo.FindByID(ctx, walletID)

	if err != nil {
		return ReconciliationOutputDTO{}, err
	}

	ledgerAmount, checkedEntries, err := u.walletRepo.CalculateLedgerBalance(ctx, walletID)
	if err != nil {
		return ReconciliationOutputDTO{}, err
	}

	storedAmount := wallet.Balance().Amount()
	differenceAmount := storedAmount - ledgerAmount
	isConsistent := differenceAmount == 0

	if !isConsistent {
		log.Printf("Reconciliation failed for wallet %s. Stored balance: %d, Calculated balance: %d, Difference: %d",
			walletID, storedAmount, ledgerAmount, differenceAmount,
		)
	}

	currency := wallet.Balance().Currency()
	storedMoney := domain.NewInternalMoney(storedAmount, currency)
	calculatedMoney := domain.NewInternalMoney(ledgerAmount, currency)
	differenceMoney := domain.NewInternalMoney(differenceAmount, currency)

	return ReconciliationOutputDTO{
		WalletID: walletID,
		StoredBalance: MoneyDTO{
			Amount:   storedMoney.String(),
			Currency: currency,
		},
		CalculatedBalance: MoneyDTO{
			Amount:   calculatedMoney.String(),
			Currency: currency,
		},
		Difference: MoneyDTO{
			Amount:   differenceMoney.String(),
			Currency: currency,
		},
		Consistent:     isConsistent,
		CheckedEntries: checkedEntries,
	}, nil
}
