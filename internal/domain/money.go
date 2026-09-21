package domain

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrIncompatibleCurrency = errors.New("incompatible currency")
	ErrMoneyOverflow        = errors.New("money overflow")
	ErrInvalidMoneyFormat   = errors.New("invalid money format")
	ErrNegativeAmountInput  = errors.New("negative amount input")
	ErrCurrencyNotSupported = errors.New("currency not supported")
)

var decimalRegex = regexp.MustCompile(`^[0-9]+\.[0-9]{2}$`)

type Money struct {
	amount   int64
	currency string
}

func (m Money) Amount() int64    { return m.amount }
func (m Money) Currency() string { return m.currency }
func NewMoneyFromString(valStr, currency string) (Money, error) {
	currency = strings.TrimSpace(strings.ToUpper(currency))
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("invalid currency format: %s", currency)
	}

	valStr = strings.TrimSpace(valStr)

	if !decimalRegex.MatchString(valStr) {
		return Money{}, ErrInvalidMoneyFormat
	}

	parts := strings.Split(valStr, ".")
	integerStr := parts[0]
	decimalStr := parts[1]

	integers, err := strconv.ParseInt(integerStr, 10, 64)
	if err != nil {
		return Money{}, ErrMoneyOverflow
	}

	decimals, err := strconv.ParseInt(decimalStr, 10, 64)
	if err != nil {
		return Money{}, ErrInvalidMoneyFormat
	}

	if integers > math.MaxInt64/100 {
		return Money{}, ErrMoneyOverflow
	}
	amount := integers * 100

	if amount > math.MaxInt64-decimals {
		return Money{}, ErrMoneyOverflow
	}
	amount += decimals
	return Money{amount: amount, currency: currency}, nil

}

func NewZeroMoney(currency string) Money {
	return Money{amount: 0, currency: strings.ToUpper(strings.TrimSpace(currency))}
}

func NewInternalMoney(amount int64, currency string) Money {
	return Money{amount: amount, currency: strings.ToUpper(strings.TrimSpace(currency))}
}

func (m Money) String() string {
	absAmount := m.amount
	sign := ""

	if m.amount < 0 {
		absAmount = -m.amount
		sign = "-"
	}

	integers := absAmount / 100
	decimals := absAmount % 100

	return fmt.Sprintf("%s%d.%02d", sign, integers, decimals)
}

func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrIncompatibleCurrency
	}

	if other.amount > 0 && m.amount > math.MaxInt64-other.amount {
		return Money{}, ErrMoneyOverflow
	}

	if other.amount < 0 && m.amount < math.MinInt64-other.amount {
		return Money{}, ErrMoneyOverflow
	}

	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrIncompatibleCurrency
	}

	if other.amount > 0 && m.amount < math.MinInt64+other.amount {
		return Money{}, ErrMoneyOverflow
	}

	if other.amount < 0 && m.amount > math.MaxInt64+other.amount {
		return Money{}, ErrMoneyOverflow
	}

	return Money{amount: m.amount - other.amount, currency: m.currency}, nil
}

func (m Money) Negate() (Money, error) {
	if m.amount == math.MinInt64 {
		return Money{}, ErrMoneyOverflow
	}
	return Money{amount: -m.amount, currency: m.currency}, nil
}

func (m Money) Compare(other Money) (int, error) {
	if m.currency != other.currency {
		return 0, ErrIncompatibleCurrency
	}

	if m.amount < other.amount {
		return -1, nil
	}

	if m.amount > other.amount {
		return 1, nil
	}

	return 0, nil
}

func (m Money) IsZero() bool {
	return m.amount == 0
}

func (m Money) IsNegative() bool {
	return m.amount < 0
}

func (m Money) IsCurrencySupported(supported []string) bool {
	for _, c := range supported {
		if strings.ToUpper(strings.TrimSpace(c)) == m.currency {
			return true
		}
	}
	return false
}
