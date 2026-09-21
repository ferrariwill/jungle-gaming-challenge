package domain

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrTerminalStateTransition                                               = errors.New("cannot change the state of a terminal transaction")
	ErrInvalidTransactionKind                                                = errors.New("invalid betting transaction type")
	ErrMissingReference                                                      = errors.New("required external reference is missing for this operation")
	ErrInvalidStateTransition                                                = errors.New("invalid state transition in state machine")
	ErrLossTypeMustHaveZeroValue                                             = errors.New("LOSS type operations must have a zero value (0.00)")
	ErrActiveExternalFinancialOperationsMustHaveValueStrictlyGreaterThanZero = errors.New("active external financial operations must have a value strictly greater than zero")
	ErrFailureCodeRequired                                                   = errors.New("failure code is required for transaction rejection")
)

type TransactionKind string

const (
	KindOpening  TransactionKind = "OPENING"
	KindBet      TransactionKind = "BET"
	KindWin      TransactionKind = "WIN"
	KindLoss     TransactionKind = "LOSS"
	KindRefund   TransactionKind = "REFUND"
	KindRollback TransactionKind = "ROLLBACK"
)

type TransactionStatus string

const (
	StatusPending          TransactionStatus = "PENDING"
	StatusPendingReference TransactionStatus = "PENDING_REFERENCE"
	StatusProcessed        TransactionStatus = "PROCESSED"
	StatusRejected         TransactionStatus = "REJECTED"
	StatusFailed           TransactionStatus = "FAILED"
)

type WagerTransaction struct {
	id                    string
	providerID            string
	externalTransactionID string
	idempotencyKey        string
	payloadHash           string
	walletID              string
	playerID              string
	roundID               string
	gameID                string
	kind                  TransactionKind
	money                 Money
	referenceExternalID   string
	status                TransactionStatus
	failureCode           string
	idempotentReplay      bool
	createdAt             time.Time
	updatedAt             time.Time
}

func NewWagerTransaction(
	id, providerID, externalID, idempotencyKey, walletID,
	playerID, roundID, gameID string,
	kind TransactionKind, money Money, referenceExternalID string,
) (*WagerTransaction, error) {

	if id == "" || providerID == "" || externalID == "" ||
		idempotencyKey == "" || walletID == "" || playerID == "" {
		return nil, ErrMissingReference
	}

	//Validacao de tipos enviados
	switch kind {
	case KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		// Permitidos externamente
	case KindOpening:
		return nil, ErrInvalidTransactionKind
	default:
		return nil, ErrInvalidTransactionKind
	}

	// Validacao de regras por tipo de transacao
	switch kind {
	case KindLoss:
		if !money.IsZero() {
			return nil, ErrLossTypeMustHaveZeroValue
		}
	case KindBet, KindWin, KindRefund, KindRollback:
		if money.IsZero() || money.IsNegative() {
			return nil, ErrActiveExternalFinancialOperationsMustHaveValueStrictlyGreaterThanZero
		}
	}

	// Validacao de referencias obrigatorias
	if (kind == KindRefund || kind == KindRollback) && strings.TrimSpace(referenceExternalID) == "" {
		return nil, ErrMissingReference
	}

	now := time.Now().UTC()

	//Inicia o status como PENDING
	initialStatus := StatusPending

	return &WagerTransaction{
		id:                    id,
		providerID:            providerID,
		externalTransactionID: externalID,
		idempotencyKey:        idempotencyKey,
		walletID:              walletID,
		playerID:              playerID,
		roundID:               roundID,
		gameID:                gameID,
		kind:                  kind,
		money:                 money,
		referenceExternalID:   referenceExternalID,
		status:                initialStatus,
		createdAt:             now,
		updatedAt:             now,
	}, nil
}

func RehydrateWagerTransaction(
	id, providerID, externalID, idempotencyKey, payloadHash, walletID, playerID,
	roundID, gameID string, kind TransactionKind, money Money, referenceExternalID string,
	status TransactionStatus, failureCode string,
	createdAt, updatedAt time.Time,
) *WagerTransaction {
	return &WagerTransaction{
		id:                    id,
		providerID:            providerID,
		externalTransactionID: externalID,
		idempotencyKey:        idempotencyKey,
		payloadHash:           payloadHash,
		walletID:              walletID,
		playerID:              playerID,
		roundID:               roundID,
		gameID:                gameID,
		kind:                  kind,
		money:                 money,
		referenceExternalID:   referenceExternalID,
		status:                status,
		failureCode:           failureCode,
		createdAt:             createdAt,
		updatedAt:             updatedAt,
	}
}

// Getters publicos encapsulados e imutaveis
func (t *WagerTransaction) ID() string {
	return t.id
}
func (t *WagerTransaction) ProviderID() string {
	return t.providerID
}
func (t *WagerTransaction) ExternalTransactionID() string {
	return t.externalTransactionID
}
func (t *WagerTransaction) IdempotencyKey() string {
	return t.idempotencyKey
}
func (t *WagerTransaction) PayloadHash() string {
	return t.payloadHash
}
func (t *WagerTransaction) WalletID() string {
	return t.walletID
}
func (t *WagerTransaction) PlayerID() string {
	return t.playerID
}
func (t *WagerTransaction) RoundID() string {
	return t.roundID
}
func (t *WagerTransaction) GameID() string {
	return t.gameID
}
func (t *WagerTransaction) Kind() TransactionKind {
	return t.kind
}
func (t *WagerTransaction) Money() Money {
	return t.money
}
func (t *WagerTransaction) ReferenceExternalID() string {
	return t.referenceExternalID
}
func (t *WagerTransaction) Status() TransactionStatus {
	return t.status
}
func (t *WagerTransaction) FailureCode() string {
	return t.failureCode
}
func (t *WagerTransaction) IdempotentReplay() bool {
	return t.idempotentReplay
}
func (t *WagerTransaction) CreatedAt() time.Time {
	return t.createdAt
}
func (t *WagerTransaction) UpdatedAt() time.Time {
	return t.updatedAt
}

// Marcacao para mostrar que veio de uma consulta de cache
func (t *WagerTransaction) MarkAsIdempotentReplay() {
	t.idempotentReplay = true
}

// Metodo para marcar uma transacao em estado definitivo
func (t *WagerTransaction) IsTerminal() bool {
	return t.status == StatusProcessed || t.status == StatusRejected || t.status == StatusFailed
}

// Metodo para alterar status para PROCESSED (sucesso)
func (t *WagerTransaction) TransitionToProcessed() error {
	if t.IsTerminal() {
		return ErrTerminalStateTransition
	}
	t.status = StatusProcessed
	t.updatedAt = time.Now().UTC()
	return nil
}

// Metodo para alterar status para PENDING_REFERENCE
func (t *WagerTransaction) TransitionToPendingReference() error {
	if t.IsTerminal() {
		return ErrTerminalStateTransition
	}
	if t.status != StatusPending && t.status != StatusPendingReference {
		return ErrInvalidStateTransition
	}
	t.status = StatusPendingReference
	t.updatedAt = time.Now().UTC()
	return nil
}

// Metodo para alterar status para REJECTED
func (t *WagerTransaction) TransitionToRejected(failureCode string) error {
	if t.IsTerminal() {
		return ErrTerminalStateTransition
	}
	if failureCode == "" {
		return ErrFailureCodeRequired
	}
	t.status = StatusRejected
	t.failureCode = failureCode
	t.updatedAt = time.Now().UTC()
	return nil
}

// Metodo para calcular o hash do payload e garantir a equivalencia absoluta
func (t *WagerTransaction) CalculateAndSetPayloadHash() error {
	payloadMap := map[string]string{
		"providerID":            t.providerID,
		"externalTransactionID": t.externalTransactionID,
		"playerID":              t.playerID,
		"walletID":              t.walletID,
		"roundID":               t.roundID,
		"gameID":                t.gameID,
		"kind":                  string(t.kind),
		"amount":                t.money.String(),
		"currency":              t.money.Currency(),
		"referenceExternalID":   t.referenceExternalID,
	}

	canonicalJSON, err := json.Marshal(payloadMap)
	if err != nil {
		return fmt.Errorf("failed to serialize canonical payload: %w", err)
	}

	hash := sha256.Sum256(canonicalJSON)
	t.payloadHash = fmt.Sprintf("%x", hash)
	return nil
}
