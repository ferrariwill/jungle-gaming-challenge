package domain

import (
	"errors"
	"time"
)

var (
	ErrWalletNotFound            = errors.New("wallet not found")
	ErrWalletAlreadyExists       = errors.New("wallet already exists")
	ErrWalletBalanceTooLow       = errors.New("wallet balance too low")
	ErrWalletBalanceTooHigh      = errors.New("wallet balance too high")
	ErrWalletBalanceNotZero      = errors.New("wallet balance not zero")
	ErrWalletBalanceNotPositive  = errors.New("wallet balance not positive")
	ErrWalletBalanceNotNegative  = errors.New("wallet balance not negative")
	ErrIdAndPlayerIDRequired     = errors.New("id and playerID are required")
	ErrNegativeOrZeroAmountInput = errors.New("amount must be positive")
	ErrWalletCurrencyMismatch    = errors.New("wallet currency mismatch")
	ErrInsufficientBalance       = errors.New("insufficient balance")
)

type Wallet struct {
	id        string
	playerID  string
	balance   Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

func NewWallet(id, playerID string, initialBalance Money) (*Wallet, error) {

	if id == "" || playerID == "" {
		return nil, ErrIdAndPlayerIDRequired
	}

	if initialBalance.IsNegative() {
		return nil, ErrNegativeAmountInput
	}

	now := time.Now().UTC()
	return &Wallet{
		id:        id,
		playerID:  playerID,
		balance:   initialBalance,
		version:   1,
		createdAt: now,
		updatedAt: now,
	}, nil

}

func RehydrateWallet(id, playerID string, balance Money, version int64, createdAt, updatedAt time.Time) (*Wallet, error) {
	if id == "" || playerID == "" {
		return nil, ErrIdAndPlayerIDRequired
	}

	if balance.IsNegative() {
		return nil, ErrNegativeAmountInput
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}, nil
}

func (w *Wallet) ID() string           { return w.id }
func (w *Wallet) PlayerID() string     { return w.playerID }
func (w *Wallet) Balance() Money       { return w.balance }
func (w *Wallet) Version() int64       { return w.version }
func (w *Wallet) CreatedAt() time.Time { return w.createdAt }
func (w *Wallet) UpdatedAt() time.Time { return w.updatedAt }

func (w *Wallet) Debit(amount Money) error {
	if amount.IsNegative() || amount.IsZero() {
		return ErrNegativeOrZeroAmountInput
	}

	if w.balance.Currency() != amount.Currency() {
		return ErrWalletCurrencyMismatch
	}

	comparison, err := w.balance.Compare(amount)
	if err != nil {
		return err
	}

	if comparison < 0 {
		return ErrInsufficientBalance
	}

	newBalance, err := w.balance.Subtract(amount)
	if err != nil {
		return err
	}
	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()
	return nil
}

func (w *Wallet) Credit(amount Money) error {
	if amount.IsNegative() || amount.IsZero() {
		return ErrNegativeOrZeroAmountInput
	}

	if w.balance.Currency() != amount.Currency() {
		return ErrWalletCurrencyMismatch
	}

	newBalance, err := w.balance.Add(amount)
	if err != nil {
		return err
	}
	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()
	return nil
}
