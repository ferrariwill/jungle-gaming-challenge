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
	ErrReferenceNotFound   = errors.New("referenced transaction not found")
	ErrReferenceNotUsable  = errors.New("referenced transaction is not in a usable state")
)

type InboxClaim struct {
	ConsumerName string
	MessageID    string
	PayloadHash  string
}

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

func NewWagerUsecase(
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
	return u.ProcessTransactionWithInbox(ctx, input, nil)
}

func (u *WagerUsecase) ProcessTransactionWithInbox(ctx context.Context, input InputTransactionDTO, inbox *InboxClaim) (OutputTransactionDTO, error) {
	money, err := domain.NewMoneyFromString(input.Amount, input.Currency)
	if err != nil {
		return OutputTransactionDTO{}, fmt.Errorf("failed: %w", err)
	}

	if !money.IsCurrencySupported(u.cfg.SupportedCurrencies) {
		return OutputTransactionDTO{}, domain.ErrIncompatibleCurrency
	}

	kind := domain.TransactionKind(input.Kind)
	internalTxID := fmt.Sprintf("tx_%s_%s", input.ProviderID, input.ExternalTransactionID)

	wagerTx, err := domain.NewWagerTransaction(
		internalTxID, input.ProviderID, input.ExternalTransactionID, input.IdempotencyKey,
		input.WalletID, input.PlayerID, input.RoundID, input.GameID,
		kind, money, input.ReferenceExternalID,
	)
	if err != nil {
		return OutputTransactionDTO{}, fmt.Errorf("failed: %w", err)
	}
	if err := wagerTx.CalculateAndSetPayloadHash(); err != nil {
		return OutputTransactionDTO{}, err
	}

	var finalWallet *domain.Wallet
	var resultTx *domain.WagerTransaction
	idempotentReplay := false

	err = u.tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
		if inbox != nil {
			if err := u.messageRepo.SaveInbox(ctx, tx, inbox.ConsumerName, inbox.MessageID, inbox.PayloadHash); err != nil {
				if errors.Is(err, repository.ErrDuplicateMessage) {
					existing, findErr := u.txRepo.FindByExternalIDTx(ctx, tx, input.ProviderID, input.ExternalTransactionID)
					if findErr != nil {
						return findErr
					}
					if existing != nil {
						wallet, wErr := u.walletRepo.FindByIDWithLock(ctx, tx, existing.WalletID())
						if wErr != nil {
							return wErr
						}
						existing.MarkAsIdempotentReplay()
						resultTx = existing
						finalWallet = wallet
						idempotentReplay = true
						return nil
					}
					// Duplicate inbox without persisted tx: acknowledge without reprocessing side effects.
					idempotentReplay = true
					resultTx = wagerTx
					wallet, wErr := u.walletRepo.FindByIDWithLock(ctx, tx, input.WalletID)
					if wErr != nil {
						return wErr
					}
					finalWallet = wallet
					return nil
				}
				return err
			}
		}

		existing, err := u.lookupExisting(ctx, tx, input, wagerTx.PayloadHash())
		if err != nil {
			return err
		}
		if existing != nil {
			wallet, wErr := u.walletRepo.FindByIDWithLock(ctx, tx, existing.WalletID())
			if wErr != nil {
				return wErr
			}
			existing.MarkAsIdempotentReplay()
			resultTx = existing
			finalWallet = wallet
			idempotentReplay = true
			return nil
		}

		wallet, err := u.walletRepo.FindByIDWithLock(ctx, tx, wagerTx.WalletID())
		if err != nil {
			return err
		}
		if wallet.PlayerID() != wagerTx.PlayerID() {
			return ErrWalletOwnerMismatch
		}

		expectedVersion := wallet.Version()
		balanceBefore := wallet.Balance().Amount()
		direction := ""
		updateWallet := false
		pendingReference := false

		switch wagerTx.Kind() {
		case domain.KindBet:
			if err := wallet.Debit(wagerTx.Money()); err != nil {
				if errors.Is(err, domain.ErrInsufficientBalance) {
					if err := wagerTx.TransitionToRejected("INSUFFICIENT_BALANCE"); err != nil {
						return err
					}
					if err := u.txRepo.Save(ctx, tx, wagerTx); err != nil {
						return u.handleSaveConflict(ctx, tx, input, wagerTx, err, &resultTx, &finalWallet, &idempotentReplay)
					}
					if err := u.saveOutboxEvent(ctx, tx, wagerTx.ID(), "WagerTransactionRejected", wagerTx); err != nil {
						return err
					}
					resultTx = wagerTx
					finalWallet = wallet
					return nil
				}
				return err
			}
			direction = "DEBIT"
			updateWallet = true

		case domain.KindWin:
			if err := wallet.Credit(wagerTx.Money()); err != nil {
				return err
			}
			direction = "CREDIT"
			updateWallet = true

		case domain.KindLoss:
			// LOSS is informational with zero money movement.

		case domain.KindRefund, domain.KindRollback:
			refTx, err := u.txRepo.FindByExternalIDTx(ctx, tx, wagerTx.ProviderID(), wagerTx.ReferenceExternalID())
			if err != nil {
				return err
			}
			if refTx == nil {
				pendingReference = true
			} else if refTx.Status() != domain.StatusProcessed {
				return ErrReferenceNotUsable
			} else {
				if err := wallet.Credit(wagerTx.Money()); err != nil {
					return err
				}
				direction = "CREDIT"
				updateWallet = true
			}

		default:
			return fmt.Errorf("invalid transaction kind: %s", wagerTx.Kind())
		}

		if pendingReference {
			if err := wagerTx.TransitionToPendingReference(); err != nil {
				return err
			}
			retryAt := time.Now().UTC().Add(5 * time.Second)
			if err := u.txRepo.SaveWithRetryAt(ctx, tx, wagerTx, &retryAt); err != nil {
				return u.handleSaveConflict(ctx, tx, input, wagerTx, err, &resultTx, &finalWallet, &idempotentReplay)
			}
			if err := u.saveOutboxEvent(ctx, tx, wagerTx.ID(), "WagerTransactionPendingReference", wagerTx); err != nil {
				return err
			}
			resultTx = wagerTx
			finalWallet = wallet
			return nil
		}

		if err := wagerTx.TransitionToProcessed(); err != nil {
			return err
		}
		if err := u.txRepo.Save(ctx, tx, wagerTx); err != nil {
			return u.handleSaveConflict(ctx, tx, input, wagerTx, err, &resultTx, &finalWallet, &idempotentReplay)
		}

		if updateWallet {
			if err := u.walletRepo.Update(ctx, tx, wallet, expectedVersion); err != nil {
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
			if err := u.saveOutboxEvent(ctx, tx, wallet.ID(), "WalletBalanceChanged", balanceChangedPayload); err != nil {
				return err
			}
		}

		if err := u.saveOutboxEvent(ctx, tx, wagerTx.ID(), "WagerTransactionProcessed", wagerTx); err != nil {
			return err
		}

		resultTx = wagerTx
		finalWallet = wallet
		return nil
	})

	if err != nil {
		return OutputTransactionDTO{}, err
	}

	return OutputTransactionDTO{
		TransactionID: resultTx.ID(),
		Status:        string(resultTx.Status()),
		Balance: MoneyDTO{
			Amount:   finalWallet.Balance().String(),
			Currency: finalWallet.Balance().Currency(),
		},
		IdempotentReplay: idempotentReplay,
	}, nil
}

func (u *WagerUsecase) ResolvePendingReference(ctx context.Context, pendingID string) error {
	return u.tm.ExecuteInTransaction(ctx, func(tx pgx.Tx) error {
		pending, err := u.txRepo.FindByIDForUpdate(ctx, tx, pendingID)
		if err != nil {
			return err
		}
		if pending == nil || pending.Status() != domain.StatusPendingReference {
			return nil
		}

		refTx, err := u.txRepo.FindByExternalIDTx(ctx, tx, pending.ProviderID(), pending.ReferenceExternalID())
		if err != nil {
			return err
		}
		if refTx == nil {
			retryAt := time.Now().UTC().Add(5 * time.Second)
			return u.txRepo.UpdateStatusWithRetryAt(ctx, tx, pending, &retryAt)
		}
		if refTx.Status() != domain.StatusProcessed {
			if err := pending.TransitionToRejected("INVALID_REFERENCE"); err != nil {
				return err
			}
			return u.txRepo.UpdateStatus(ctx, tx, pending)
		}

		wallet, err := u.walletRepo.FindByIDWithLock(ctx, tx, pending.WalletID())
		if err != nil {
			return err
		}
		expectedVersion := wallet.Version()
		balanceBefore := wallet.Balance().Amount()

		if err := wallet.Credit(pending.Money()); err != nil {
			return err
		}
		if err := pending.TransitionToProcessed(); err != nil {
			return err
		}
		if err := u.txRepo.UpdateStatus(ctx, tx, pending); err != nil {
			return err
		}
		if err := u.walletRepo.Update(ctx, tx, wallet, expectedVersion); err != nil {
			return err
		}
		ledgerEntry := repository.IDLEntryDTO{
			ID:            fmt.Sprintf("led_%s", pending.ID()),
			WalletID:      wallet.ID(),
			TransactionID: pending.ID(),
			Direction:     "CREDIT",
			Amount:        pending.Money().Amount(),
			BalanceBefore: balanceBefore,
			BalanceAfter:  wallet.Balance().Amount(),
		}
		if err := u.txRepo.SaveLedgerEntry(ctx, tx, ledgerEntry); err != nil {
			return err
		}
		if err := u.saveOutboxEvent(ctx, tx, pending.ID(), "WagerTransactionProcessed", pending); err != nil {
			return err
		}
		balanceChangedPayload := map[string]interface{}{
			"walletId":      wallet.ID(),
			"transactionId": pending.ID(),
			"direction":     "CREDIT",
			"amount":        pending.Money().String(),
			"currency":      pending.Money().Currency(),
			"balanceBefore": domain.NewInternalMoney(balanceBefore, wallet.Balance().Currency()).String(),
			"balanceAfter":  wallet.Balance().String(),
			"walletVersion": wallet.Version(),
		}
		return u.saveOutboxEvent(ctx, tx, wallet.ID(), "WalletBalanceChanged", balanceChangedPayload)
	})
}

func (u *WagerUsecase) lookupExisting(ctx context.Context, tx pgx.Tx, input InputTransactionDTO, expectedHash string) (*domain.WagerTransaction, error) {
	byExternal, err := u.txRepo.FindByExternalIDTx(ctx, tx, input.ProviderID, input.ExternalTransactionID)
	if err != nil {
		return nil, err
	}
	if byExternal != nil {
		if byExternal.PayloadHash() != expectedHash {
			return nil, repository.ErrTransactionConflict
		}
		return byExternal, nil
	}

	byKey, err := u.txRepo.FindByIdempotencyKey(ctx, tx, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if byKey != nil {
		if byKey.PayloadHash() != expectedHash {
			return nil, repository.ErrTransactionConflict
		}
		return byKey, nil
	}
	return nil, nil
}

func (u *WagerUsecase) handleSaveConflict(
	ctx context.Context,
	tx pgx.Tx,
	input InputTransactionDTO,
	wagerTx *domain.WagerTransaction,
	saveErr error,
	resultTx **domain.WagerTransaction,
	finalWallet **domain.Wallet,
	idempotentReplay *bool,
) error {
	if !errors.Is(saveErr, repository.ErrDuplicateTransaction) {
		return saveErr
	}
	existing, err := u.lookupExisting(ctx, tx, input, wagerTx.PayloadHash())
	if err != nil {
		return err
	}
	if existing == nil {
		return repository.ErrTransactionConflict
	}
	wallet, err := u.walletRepo.FindByIDWithLock(ctx, tx, existing.WalletID())
	if err != nil {
		return err
	}
	existing.MarkAsIdempotentReplay()
	*resultTx = existing
	*finalWallet = wallet
	*idempotentReplay = true
	return nil
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
