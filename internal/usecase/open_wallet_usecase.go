package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/jackc/pgx/v5"
)

type OpenWalletInputDTO struct {
	PlayerID        string `json:"playerId"`
	InitialAmount   string `json:"initialAmount"`
	InitialCurrency string `json:"initialCurrency"`
}

type OpenWalletOutputDTO struct {
	ID       string   `json:"id"`
	PlayerID string   `json:"playerId"`
	Balance  MoneyDTO `json:"balance"`
	Version  int64    `json:"version"`
}

type OpenWalletUsecase struct {
	cfg         *config.Config
	tm          *database.TransactionManager
	walletRepo  *repository.WalletRepository
	txRepo      *repository.TransactionRepository
	messageRepo *repository.MessagingRepository
}

func NewOpenWalletUseCase(
	cfg *config.Config,
	tm *database.TransactionManager,
	walletRepo *repository.WalletRepository,
	txRepo *repository.TransactionRepository,
	messageRepo *repository.MessagingRepository) *OpenWalletUsecase {
	return &OpenWalletUsecase{
		cfg:         cfg,
		tm:          tm,
		walletRepo:  walletRepo,
		txRepo:      txRepo,
		messageRepo: messageRepo,
	}
}

func (u *OpenWalletUsecase) saveOutbox(ctx context.Context, tx pgx.Tx, aggregateID, eventType string, data interface{}) error {
	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return u.messageRepo.SaveOutbox(ctx, tx, repository.OutboxEventDTO{
		ID:          fmt.Sprintf("evt_%s_%d", aggregateID, now.UnixNano()),
		AggregateID: aggregateID,
		EventType:   eventType,
		Payload:     string(payloadBytes),
		Status:      "PENDING",
		NextSendAt:  now,
		CreatedAt:   now,
	})
}

func (u *OpenWalletUsecase) Execute(ctx context.Context, input OpenWalletInputDTO) (OpenWalletOutputDTO, error) {
	initialMoney, err := domain.NewMoneyFromString(input.InitialAmount, input.InitialCurrency)
	if err != nil {
		return OpenWalletOutputDTO{}, err
	}

	if !initialMoney.IsCurrencySupported(u.cfg.SupportedCurrencies) {
		return OpenWalletOutputDTO{}, domain.ErrIncompatibleCurrency
	}

	walletID := fmt.Sprintf("wal_%s_%s", input.PlayerID, strings.ToLower(input.InitialCurrency))

	wallet, err := domain.NewWallet(walletID, input.PlayerID, initialMoney)
	if err != nil {
		return OpenWalletOutputDTO{}, err
	}

	err = u.tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {

		if err := u.walletRepo.Save(ctx, tx, wallet); err != nil {
			return err
		}

		if !initialMoney.IsZero() {
			openingTxID := fmt.Sprintf("tx_opening_%s", wallet.ID())
			externalID := fmt.Sprintf("opening_%s", wallet.ID())
			idemKey := fmt.Sprintf("opening:%s", wallet.ID())

			wagerTx := domain.RehydrateWagerTransaction(
				openingTxID, "INTERNAL", externalID, idemKey, "",
				wallet.ID(), wallet.PlayerID(), "INTERNAL", "INTERNAL",
				domain.KindOpening, initialMoney, "", domain.StatusProcessed, "",
				time.Now().UTC(), time.Now().UTC(),
			)
			_ = wagerTx.CalculateAndSetPayloadHash()

			if err := u.txRepo.Save(ctx, tx, wagerTx); err != nil {
				return err
			}

			ledgerEntry := repository.IDLEntryDTO{
				ID:            fmt.Sprintf("led_%s", wagerTx.ID()),
				WalletID:      wallet.ID(),
				TransactionID: wagerTx.ID(),
				Direction:     "CREDIT",
				Amount:        initialMoney.Amount(),
				BalanceBefore: 0,
				BalanceAfter:  initialMoney.Amount(),
			}

			if err := u.txRepo.SaveLedgerEntry(ctx, tx, ledgerEntry); err != nil {
				return err
			}

			if err := u.saveOutbox(ctx, tx, wallet.ID(), "WALLET_OPENED", wallet); err != nil {
				return err
			}
			balancePayload := map[string]interface{}{
				"walletId":      wallet.ID(),
				"playerId":      wallet.PlayerID(),
				"direction":     "CREDIT",
				"amount":        initialMoney.Amount(),
				"currency":      initialMoney.Currency(),
				"balanceBefore": "0.00",
				"balanceAfter":  initialMoney.Amount(),
				"walletVersion": wallet.Version(),
			}
			if err := u.saveOutbox(ctx, tx, wallet.ID(), "WalletBalanceChanged", balancePayload); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return OpenWalletOutputDTO{}, err
	}

	return OpenWalletOutputDTO{
		ID:       wallet.ID(),
		PlayerID: wallet.PlayerID(),
		Balance: MoneyDTO{
			Amount:   wallet.Balance().String(),
			Currency: wallet.Balance().Currency(),
		},
		Version: wallet.Version(),
	}, nil
}
