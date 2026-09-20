package domain_test

import (
	"_/D_/junglegaming/internal/domain"
	"math"
	"testing"
)

func TestNewMoneyFromString_Valid(t *testing.T) {
	m, err := domain.NewMoneyFromStrig("25.00", "BRL")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if m.Amount() != 2500 || m.Currency() != "BRL" {
		t.Fatalf("Expected amount 2500 and currency BRL, got %d and %s", m.Amount(), m.Currency())
	}

	if m.String() != "25.00" {
		t.Fatalf("Expected string 25.00, got %s", m.String())
	}
}

func TestNewMoneyFromStrings_Invalid(t *testing.T) {
	invalidInputs := []string{"25", "25.0", "25.000", "abc", "25,00", "NaN", "Infinity", "2.5e+01"}
	for _, input := range invalidInputs {
		_, err := domain.NewMoneyFromStrig(input, "BRL")
		if err == nil {
			t.Fatalf("Expected error for input %s, got nil", input)
		}
	}
}

func TestMoney_ArithmeticOverflows(t *testing.T) {
	mMax := domain.NewInternalMoney(math.MaxInt64, "BRL")
	one := domain.NewInternalMoney(1, "BRL")
	_, err := mMax.Add(one)
	if err == nil {
		t.Fatalf("Expected overflow error, got nil")
	}

	mMin := domain.NewInternalMoney(math.MinInt64, "BRL")
	_, err = mMin.Subtract(one)
	if err == nil {
		t.Fatalf("Expected overflow error, got nil")
	}

	mZero := domain.NewInternalMoney(0, "BRL")
	_, err = mZero.Negate()
	if err == nil {
		t.Fatalf("Expected overflow error, got nil")
	}
}

func TestMoney_CurrencyIncompatibility(t *testing.T) {
	brl := domain.NewZeroMoney("BRL")
	usd := domain.NewZeroMoney("USD")
	_, err := brl.Add(usd)
	if err == nil {
		t.Fatalf("Expected currency incompatibility error, got nil")
	}
	_, err = brl.Subtract(usd)
	if err == nil {
		t.Fatalf("Expected currency incompatibility error, got nil")
	}
	_, err = brl.Compare(usd)
	if err == nil {
		t.Fatalf("Expected currency incompatibility error, got nil")
	}
}
