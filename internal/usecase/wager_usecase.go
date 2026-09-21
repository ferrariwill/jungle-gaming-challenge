package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/jackc/pgx/v5"
)

var (
	ErrWalletOwnerMismatch = errors.New("the specified wallet does not belong to the given player")
)

type InputTransactionDTO struct {
	ProviderID            string
	ExternalTransactionID string
	IdempotencyKey        string
	PlayerID              string
	WalletID              string
	RoundID               string
	GameID                string
	Kind                  string
	Amount                string
	Currency              string
	ReferenceExternalID   string
}

type OutputTransactionDTO struct {
	TransactionID    string   `json:"transactionId"`
	Status           string   `json:"status"`
	Balance          MoneyDTO `json:"balance"`
	IdempotentReplay bool     `json:"idempotentReplay"`
}

type MoneyDTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type WagerUsecase struct {
	cfg         *config.Config
	tm          *database.TransactionManager
	walletRepo  *repository.WalletRepository
	txRepo      *repository.TransactionRepository
	messageRepo *repository.MessagingRepository
}

func NewWagerUseCase(
	cfg *config.Config,
	tm *database.TransactionManager,
	walletRepo *repository.WalletRepository,
	txRepo *repository.TransactionRepository,
	messageRepo *repository.MessagingRepository,
) *WagerUsecase {
	return &WagerUsecase{
		cfg:         cfg,
		tm:          tm,
		walletRepo:  walletRepo,
		txRepo:      txRepo,
		messageRepo: messageRepo,
	}
}

func (u *WagerUsecase) ProcessTransaction(ctx context.Context, input InputTransactionDTO) (OutputTransactionDTO, error) {
	money, err := domain.NewMoneyFromStrig(input.Amount, input.Currency)

	if err != nil {
		return OutputTransactionDTO{}, fmt.Errorf("failed: %w", err)
	}

	if !money.IsCurrencySupported(u.cfg.SupportedCurrencies) {
		return OutputTransactionDTO{}, domain.ErrIncompatibleCurrency
	}

	existingTx, err := u.txRepo.FindByExternalID(ctx, input.ProviderID, input.ExternalTransactionID)
	if err != nil {
		return OutputTransactionDTO{}, fmt.Errorf("failed: %w", err)
	}

	if existingTx != nil {
		//Gera transação temporaria para validar o hash do payload
		tempTx, _ := domain.NewWagerTransaction(
			existingTx.ID(),
			input.ProviderID,
			input.ExternalTransactionID,
			input.IdempotencyKey,
			input.WalletID,
			input.PlayerID,
			input.RoundID,
			input.GameID,
			domain.TransactionKind(input.Kind),
			money,
			input.ReferenceExternalID,
		)

		_ = tempTx.CalculateAndSetPayloadHash()

		// Se a chave bateu mais teve alteração de payload, retorna erro
		if existingTx.PayloadHash() != tempTx.PayloadHash() {
			return OutputTransactionDTO{}, repository.ErrTransactionConflict
		}

		wallet, err := u.walletRepo.FindByID(ctx, existingTx.WalletID())
		if err != nil {
			return OutputTransactionDTO{}, fmt.Errorf("failed: %w", err)
		}

		return OutputTransactionDTO{
			TransactionID: existingTx.ID(),
			Status:        string(existingTx.Status()),
			Balance: MoneyDTO{
				Amount:   wallet.Balance().String(),
				Currency: existingTx.Money().Currency(),
			},
			IdempotentReplay: true,
		}, nil
	}

	// Instancia a nova Transação no Dominio e calcula o hash do payload
	internalTxID := fmt.Sprintf("tx_%s_%s", input.ProviderID, input.ExternalTransactionID)

	wagerTx, err := domain.NewWagerTransaction(
		internalTxID, input.ProviderID, input.ExternalTransactionID, input.IdempotencyKey,
		input.WalletID, input.PlayerID, input.RoundID, input.GameID,
		domain.TransactionKind(input.Kind), money, input.ReferenceExternalID,
	)

	if err != nil {
		return OutputTransactionDTO{}, fmt.Errorf("failed: %w", err)
	}

	_ = wagerTx.CalculateAndSetPayloadHash()

	var finalWallet *domain.Wallet

	err = u.tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
		//Aplica o Lock na linha da carteira, para enfileirar possiveis acessos concorrentes
		wallet, err := u.walletRepo.FindByIDWithLock(ctx, tx, wagerTx.WalletID())
		if err != nil {
			return err
		}

		//Valida se o player é o proprietario da carteira
		if wallet.PlayerID() != wagerTx.PlayerID() {
			return ErrWalletOwnerMismatch
		}

		var direction string
		balanceBefore := wallet.Balance().Amount()

		//Executa a regra de negocio no tipo de transação
		switch wagerTx.Kind() {
		case domain.KindBet:
			if err := wallet.Debit(wagerTx.Money()); err != nil {
				if errors.Is(err, domain.ErrInsufficientBalance) {
					_ = wagerTx.TransitionToRejected("INSUFFICIENT_BALANCE")
					_ = u.txRepo.Save(ctx, tx, wagerTx)
					finalWallet = wallet

					_ = u.saveOutboxEvent(ctx, tx, wagerTx.ID(), "WagerTransactionRejected", wagerTx)
					return nil
				}
				return err
			}
			direction = "DEBIT"
		case domain.KindWin:
			if err := wallet.Credit(wagerTx.Money()); err != nil {
				return err
			}
			direction = "CREDIT"
		default:
			return fmt.Errorf("invalid transaction kind: %s", wagerTx.Kind())
		}

		if err := wagerTx.TransitionToProcessed(); err != nil {
			return err
		}

		if err := u.txRepo.Save(ctx, tx, wagerTx); err != nil {
			return err
		}

		if err := u.walletRepo.Update(ctx, tx, wallet); err != nil {
			return err
		}

		ledgerEntry := repository.IDLEntryDTO{
			ID:            fmt.Sprintf("led_%s", wagerTx.ID()),
			WalletID:      wallet.ID(),
			TransactionID: wagerTx.ID(),
			Direction:     direction,
			Amount:        wagerTx.Money().Amount(),
			BalanceBefore: balanceBefore,
			BalanceAfter:  wallet.Balance().Amount(),
		}

		if err := u.txRepo.SaveLedgerEntry(ctx, tx, ledgerEntry); err != nil {
			return err
		}

		err = u.saveOutboxEvent(ctx, tx, wagerTx.ID(), "WagerTransactionProcessed", wagerTx)
		if err != nil {
			return err
		}

		balanceChangedPayload := map[string]interface{}{
			"walletId":      wallet.ID(),
			"transactionId": wagerTx.ID(),
			"direction":     direction,
			"amount":        wagerTx.Money().String(),
			"currency":      wagerTx.Money().Currency(),
			"balanceBefore": domain.NewInternalMoney(balanceBefore, wallet.Balance().Currency()).String(),
			"balanceAfter":  wallet.Balance().String(),
			"walletVersion": wallet.Version(),
		}
		err = u.saveOutboxEvent(ctx, tx, wallet.ID(), "WalletBalanceChanged", balanceChangedPayload)
		if err != nil {
			return err
		}

		finalWallet = wallet
		return nil
	})

	if err != nil {
		return OutputTransactionDTO{}, err
	}

	return OutputTransactionDTO{
		TransactionID: wagerTx.ID(),
		Status:        string(wagerTx.Status()),
		Balance: MoneyDTO{
			Amount:   finalWallet.Balance().String(),
			Currency: finalWallet.Balance().Currency(),
		},
		IdempotentReplay: false,
	}, nil
}

func (u *WagerUsecase) saveOutboxEvent(ctx context.Context, tx pgx.Tx, aggregateID string, eventName string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize event to outbox: %w", err)
	}

	now := time.Now().UTC()
	eventDTO := repository.OutboxEventDTO{
		ID:          fmt.Sprintf("evt_%s_%d", aggregateID, now.UnixNano()),
		AggregateID: aggregateID,
		EventType:   eventName,
		Payload:     string(payloadBytes),
		Status:      "PENDING",
		NextSendAt:  now,
		CreatedAt:   now,
	}
	return u.messageRepo.SaveOutbox(ctx, tx, eventDTO)
}
