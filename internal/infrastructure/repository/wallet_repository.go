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
	ErrWalletNotFound = errors.New("wallet not found")
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{
		pool: pool,
	}
}

func (r *WalletRepository) Save(ctx context.Context, tx pgx.Tx, wallet *domain.Wallet) error {
	query := `
	INSERT INTO wallets (id, player_id, currency, amount, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.Exec(ctx, query,
		wallet.ID(),
		wallet.PlayerID(),
		wallet.Balance().Currency(),
		wallet.Balance().Amount(),
		wallet.Version(),
		wallet.CreatedAt(),
		wallet.UpdatedAt(),
	)

	if err != nil {
		return fmt.Errorf("failed to save wallet: %w", err)
	}

	return nil
}

func (r *WalletRepository) Update(ctx context.Context, tx pgx.Tx, wallet *domain.Wallet) error {
	query := `
		UPDATE wallets
		SET amount = $1, version = $2, updated_at = $3
		WHERE id = $4
	`
	result, err := tx.Exec(ctx, query,
		wallet.Balance().Amount(),
		wallet.Version(),
		wallet.UpdatedAt(),
		wallet.ID(),
	)

	if err != nil {
		return fmt.Errorf("failed to update wallet: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrWalletNotFound
	}

	return nil
}

func (r *WalletRepository) FindByIDWithLock(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error) {
	query := `
		SELECT id, player_id, currency, amount, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var (
		walletID, playerID, currency string
		amount, version              int64
		createdAt, updatedAt         time.Time
	)

	err := tx.QueryRow(ctx, query, id).Scan(
		&walletID,
		&playerID,
		&currency,
		&amount,
		&version,
		&createdAt,
		&updatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to find wallet by id: %w", err)
	}

	money := domain.NewInternalMoney(amount, currency)
	return domain.RehydrateWallet(walletID, playerID, money, version, createdAt, updatedAt)
}

func (r *WalletRepository) FindByID(ctx context.Context, id string) (*domain.Wallet, error) {
	query := `
		SELECT id, player_id, currency, amount, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	var (
		walletID, playerID, currency string
		amount, version              int64
		createdAt, updatedAt         time.Time
	)

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&walletID,
		&playerID,
		&currency,
		&amount,
		&version,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to find wallet by id: %w", err)
	}

	money := domain.NewInternalMoney(amount, currency)
	return domain.RehydrateWallet(walletID, playerID, money, version, createdAt, updatedAt)
}

func (r *WalletRepository) CalculateLedgerBalance(ctx context.Context, walletID string) (int64, int64, error) {
	query := `
		SELECT 
			COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount ELSE 0 END), 0) as total_credits,
			COALESCE(SUM(CASE WHEN direction = 'DEBIT' THEN amount ELSE 0 END), 0) as total_debits,
			COUNT(*) as total_entries
		FROM wallet_ledger_entries
		WHERE wallet_id = $1
	`
	var credits, debits, count int64
	err := r.pool.QueryRow(ctx, query, walletID).Scan(&credits, &debits, &count)

	if err != nil {
		return 0, 0, fmt.Errorf("failed to calculate ledger balance: %w", err)
	}

	calculatedBalance := credits - debits

	return calculatedBalance, count, nil
}
