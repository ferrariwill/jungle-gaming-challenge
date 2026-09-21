package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTransactionConflict  = errors.New("conflict: idempotency key reused with a different payload")
	ErrDuplicateTransaction = errors.New("duplicate transaction")
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *TransactionRepository) FindByExternalID(ctx context.Context, providerID, externalID string) (*domain.WagerTransaction, error) {
	if r.pool == nil {
		return nil, ErrNilDatabasePool
	}
	return r.findByExternalID(ctx, r.pool, providerID, externalID)
}

func (r *TransactionRepository) FindByExternalIDTx(ctx context.Context, tx pgx.Tx, providerID, externalID string) (*domain.WagerTransaction, error) {
	if tx == nil {
		return nil, ErrNilTransaction
	}
	return r.findByExternalID(ctx, tx, providerID, externalID)
}

func (r *TransactionRepository) FindByIdempotencyKey(ctx context.Context, tx pgx.Tx, idempotencyKey string) (*domain.WagerTransaction, error) {
	if tx == nil {
		return nil, ErrNilTransaction
	}
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount, currency,
		       reference_external_id, status, failure_code, created_at, updated_at
		FROM wager_transactions
		WHERE idempotency_key = $1
	`
	return r.scanOne(ctx, tx, query, idempotencyKey)
}

func (r *TransactionRepository) findByExternalID(ctx context.Context, q rowQuerier, providerID, externalID string) (*domain.WagerTransaction, error) {
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount, currency,
		       reference_external_id, status, failure_code, created_at, updated_at
		FROM wager_transactions
		WHERE provider_id = $1 AND external_transaction_id = $2
	`
	return r.scanOne(ctx, q, query, providerID, externalID)
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r *TransactionRepository) scanOne(ctx context.Context, q rowQuerier, query string, args ...any) (*domain.WagerTransaction, error) {
	var (
		id, provID, extID, idxKey, pHash, walletID, playerID, roundID, gameID, kindStr, statusStr string
		amount                                                                                    int64
		currency                                                                                  string
		refExtID, failCode                                                                        *string
		createdAt, updatedAt                                                                      time.Time
	)

	err := q.QueryRow(ctx, query, args...).Scan(
		&id, &provID, &extID, &idxKey, &pHash, &walletID, &playerID, &roundID, &gameID,
		&kindStr, &amount, &currency, &refExtID, &statusStr, &failCode, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find transaction: %w", err)
	}

	money := domain.NewInternalMoney(amount, currency)
	var rExt, fCode string
	if refExtID != nil {
		rExt = *refExtID
	}
	if failCode != nil {
		fCode = *failCode
	}

	return domain.RehydrateWagerTransaction(
		id, provID, extID, idxKey, pHash, walletID, playerID, roundID, gameID,
		domain.TransactionKind(kindStr), money, rExt, domain.TransactionStatus(statusStr), fCode,
		createdAt, updatedAt,
	), nil
}

func (r *TransactionRepository) Save(ctx context.Context, tx pgx.Tx, wager *domain.WagerTransaction) error {
	return r.SaveWithRetryAt(ctx, tx, wager, nil)
}

func (r *TransactionRepository) SaveWithRetryAt(ctx context.Context, tx pgx.Tx, wager *domain.WagerTransaction, nextRetryAt *time.Time) error {
	if tx == nil {
		return ErrNilTransaction
	}

	query := `
		INSERT INTO wager_transactions (
			id, provider_id, external_transaction_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind, amount, currency,
			reference_external_id, status, failure_code, next_retry_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
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
		nextRetryAt, wager.CreatedAt(), wager.UpdatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicateTransaction
		}
		return fmt.Errorf("failed to save transaction: %w", err)
	}
	return nil
}

func (r *TransactionRepository) UpdateStatus(ctx context.Context, tx pgx.Tx, wager *domain.WagerTransaction) error {
	return r.UpdateStatusWithRetryAt(ctx, tx, wager, nil)
}

func (r *TransactionRepository) UpdateStatusWithRetryAt(ctx context.Context, tx pgx.Tx, wager *domain.WagerTransaction, nextRetryAt *time.Time) error {
	if tx == nil {
		return ErrNilTransaction
	}

	query := `
		UPDATE wager_transactions
		SET status = $1, failure_code = $2, next_retry_at = $3, updated_at = $4
		WHERE id = $5
	`
	var failCode *string
	if wager.FailureCode() != "" {
		code := wager.FailureCode()
		failCode = &code
	}

	_, err := tx.Exec(ctx, query, string(wager.Status()), failCode, nextRetryAt, wager.UpdatedAt(), wager.ID())
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

func (r *TransactionRepository) FindPendingReferences(ctx context.Context, limit int) ([]*domain.WagerTransaction, error) {
	if r.pool == nil {
		return nil, ErrNilDatabasePool
	}

	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount, currency,
		       reference_external_id, status, failure_code, created_at, updated_at
		FROM wager_transactions
		WHERE status = 'PENDING_REFERENCE'
		  AND (next_retry_at IS NULL OR next_retry_at <= $1)
		ORDER BY created_at ASC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, time.Now().UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to find pending references: %w", err)
	}
	defer rows.Close()

	var result []*domain.WagerTransaction
	for rows.Next() {
		var (
			id, provID, extID, idxKey, pHash, walletID, playerID, roundID, gameID, kindStr, statusStr string
			amount                                                                                    int64
			currency                                                                                  string
			refExtID, failCode                                                                        *string
			createdAt, updatedAt                                                                      time.Time
		)
		if err := rows.Scan(
			&id, &provID, &extID, &idxKey, &pHash, &walletID, &playerID, &roundID, &gameID,
			&kindStr, &amount, &currency, &refExtID, &statusStr, &failCode, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan pending reference: %w", err)
		}
		money := domain.NewInternalMoney(amount, currency)
		var rExt, fCode string
		if refExtID != nil {
			rExt = *refExtID
		}
		if failCode != nil {
			fCode = *failCode
		}
		result = append(result, domain.RehydrateWagerTransaction(
			id, provID, extID, idxKey, pHash, walletID, playerID, roundID, gameID,
			domain.TransactionKind(kindStr), money, rExt, domain.TransactionStatus(statusStr), fCode,
			createdAt, updatedAt,
		))
	}
	return result, rows.Err()
}

func (r *TransactionRepository) FindByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.WagerTransaction, error) {
	if tx == nil {
		return nil, ErrNilTransaction
	}
	query := `
		SELECT id, provider_id, external_transaction_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind, amount, currency,
		       reference_external_id, status, failure_code, created_at, updated_at
		FROM wager_transactions
		WHERE id = $1
		FOR UPDATE
	`
	return r.scanOne(ctx, tx, query, id)
}
