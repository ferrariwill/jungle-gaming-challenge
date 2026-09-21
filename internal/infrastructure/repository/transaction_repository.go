package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTransactionConflict = errors.New("conflict: idempotency key reused with a different payload")
)

type IDLEntryDTO struct {
	ID            string
	WalletID      string
	TransactionID string
	Direction     string
	Amount        int64
	BalanceBefore int64
	BalanceAfter  int64
}

type TransactionRepository struct {
	pool *pgxpool.Pool
}

func NewTransactionRepository(pool *pgxpool.Pool) *TransactionRepository {
	return &TransactionRepository{
		pool: pool,
	}
}

func (r *TransactionRepository) FindByExternalID(ctx context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {

	if r.pool == nil {
		return nil, ErrNilDatabasePool
	}

	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash, 
		       wallet_id, player_id, round_id, game_id, kind, amount, currency, 
		       reference_external_id, status, failure_code, created_at, updated_at
		FROM wager_transactions
		WHERE provider_id = $1 AND external_transaction_id = $2
	`
	var (
		id, provID, extID, idxKey, pHash, walletID, playerID, roundID, gameID, kindStr, statusStr string
		amount                                                                                    int64
		currency                                                                                  string
		refExtID, failCode                                                                        *string
		createdAt, updatedAt                                                                      time.Time
	)

	err := r.pool.QueryRow(ctx, query, providerID, externalID).Scan(
		&id, &provID, &extID, &idxKey, &pHash, &walletID, &playerID, &roundID, &gameID,
		&kindStr, &amount, &currency, &refExtID, &statusStr, &failCode, &createdAt, &updatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find transaction by external id: %w", err)
	}

	money := domain.NewInternalMoney(amount, currency)

	var rExt, fCode string
	if refExtID != nil {
		rExt = *refExtID
	}

	if failCode != nil {
		fCode = *failCode
	}

	tx := domain.RehydrateWagerTransaction(
		id, provID, extID, idxKey, pHash, walletID, playerID, roundID, gameID,
		domain.TransactionKind(kindStr), money, rExt, domain.TransactionStatus(statusStr), fCode,
		createdAt, updatedAt,
	)

	return tx, nil
}

func (r *TransactionRepository) Save(ctx context.Context, tx pgx.Tx, wager *domain.WagerTransaction) error {
	if tx == nil {
		return ErrNilTransaction
	}

	query := `
		INSERT INTO wager_transactions (
			id, provider_id, external_transaction_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind, amount, currency,
			reference_external_id, status, failure_code, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`
	var refExt, failCode *string

	if wager.ReferenceExternalID() != "" {
		ref := wager.ReferenceExternalID()
		refExt = &ref
	}
	if wager.FailureCode() != "" {
		code := wager.FailureCode()
		failCode = &code
	}

	_, err := tx.Exec(ctx, query,
		wager.ID(), wager.ProviderID(), wager.ExternalTransactionID(), wager.IdempotencyKey(), wager.PayloadHash(),
		wager.WalletID(), wager.PlayerID(), wager.RoundID(), wager.GameID(), string(wager.Kind()),
		wager.Money().Amount(), wager.Money().Currency(), refExt, string(wager.Status()), failCode,
		wager.CreatedAt(), wager.UpdatedAt(),
	)

	if err != nil {
		return fmt.Errorf("failed to save transaction: %w", err)
	}

	return nil
}

func (r *TransactionRepository) UpdateStatus(ctx context.Context, tx pgx.Tx, wager *domain.WagerTransaction) error {
	if tx == nil {
		return ErrNilTransaction
	}

	query := `
		UPDATE wager_transactions 
		SET status = $1, failure_code = $2, updated_at = $3
		WHERE id = $4
	`
	var failCode *string
	if wager.FailureCode() != "" {
		code := wager.FailureCode()
		failCode = &code
	}

	_, err := tx.Exec(ctx, query, string(wager.Status()), failCode, wager.UpdatedAt(), wager.ID())
	if err != nil {
		return fmt.Errorf("failed to update transaction status: %w", err)
	}

	return nil
}

func (r *TransactionRepository) SaveLedgerEntry(ctx context.Context, tx pgx.Tx, entry IDLEntryDTO) error {
	if tx == nil {
		return ErrNilTransaction
	}

	query := `
		INSERT INTO wallet_ledger_entries (id, wallet_id, transaction_id, direction, amount, balance_before, balance_after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := tx.Exec(ctx, query,
		entry.ID, entry.WalletID, entry.TransactionID, entry.Direction,
		entry.Amount, entry.BalanceBefore, entry.BalanceAfter, time.Now().UTC(),
	)

	if err != nil {
		return fmt.Errorf("failed to save ledger entry: %w", err)
	}

	return nil
}
