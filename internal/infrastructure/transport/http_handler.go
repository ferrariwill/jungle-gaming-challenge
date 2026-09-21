package transport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/domain"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
)

type HTTPHandler struct {
	wagerUsecase      *usecase.WagerUsecase
	openWalletUsecase *usecase.OpenWalletUsecase
	walletRepo        *repository.WalletRepository
	authEvent         *AuthMiddleware
}

func NewHTTPHandler(
	wagerUsecase *usecase.WagerUsecase,
	openWalletUsecase *usecase.OpenWalletUsecase,
	walletRepo *repository.WalletRepository,
	authEvent *AuthMiddleware,
) *HTTPHandler {
	return &HTTPHandler{
		wagerUsecase:      wagerUsecase,
		openWalletUsecase: openWalletUsecase,
		walletRepo:        walletRepo,
		authEvent:         authEvent,
	}
}

func (h *HTTPHandler) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"UP"}`))
}

func (h *HTTPHandler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"READY"}`))
}

func (h *HTTPHandler) respondWithError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /wallets", h.handleCreateWallet)
	mux.HandleFunc("GET /wallets/{walletId}", h.handleGetWallet)
	mux.HandleFunc("POST /wagering/transactions", h.handleProcessTransaction)

	mux.HandleFunc("GET /health/live", h.handleLiveness)
	mux.HandleFunc("GET /health/ready", h.handleReadiness)

	//Aplica o middleware de autenticacao em todas as rotas
	h.authEvent.Authenticate(mux).ServeHTTP(w, r)
}

// Responsavel por criar uma nova carteira de jogador
func (h *HTTPHandler) handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlayerID       string `json:"playerId"`
		InitialBalance struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"initialBalance"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	output, err := h.openWalletUsecase.Execute(r.Context(), usecase.OpenWalletInputDTO{
		PlayerID:        body.PlayerID,
		InitialAmount:   body.InitialBalance.Amount,
		InitialCurrency: body.InitialBalance.Currency,
	})

	if err != nil {
		h.respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(output)
}

// Responsavel por buscar uma carteira de jogador existente
func (h *HTTPHandler) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("walletId")
	if strings.TrimSpace(walletID) == "" {
		h.respondWithError(w, http.StatusBadRequest, "Wallet ID is required")
		return
	}

	wallet, err := h.walletRepo.FindByID(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, repository.ErrWalletNotFound) {
			h.respondWithError(w, http.StatusNotFound, "Wallet not found")
			return
		}
		h.respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       wallet.ID(),
		"playerId": wallet.PlayerID(),
		"balance": map[string]string{
			"amount":   wallet.Balance().String(),
			"currency": wallet.Balance().Currency(),
		},
		"version": wallet.Version(),
	})
}

func (h *HTTPHandler) handleProcessTransaction(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(idempotencyKey) == "" {
		h.respondWithError(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}

	var body struct {
		ProviderID            string `json:"providerId"`
		ExternalTransactionID string `json:"externalTransactionId"`
		PlayerID              string `json:"playerId"`
		WalletID              string `json:"walletId"`
		RoundID               string `json:"roundId"`
		GameID                string `json:"gameId"`
		Kind                  string `json:"kind"`
		Money                 struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"money"`
		ReferenceExternalID string `json:"referenceExternalTransactionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctxProviderID, ok := r.Context().Value(ProviderIDKey).(string)
	if !ok {
		h.respondWithError(w, http.StatusInternalServerError, "Provider ID not found in context")
		return
	}

	if body.ProviderID != ctxProviderID {
		h.respondWithError(w, http.StatusForbidden, "Access denied: Provider ID mismatch")
		return
	}

	input := usecase.InputTransactionDTO{
		ProviderID:            body.ProviderID,
		ExternalTransactionID: body.ExternalTransactionID,
		IdempotencyKey:        idempotencyKey,
		PlayerID:              body.PlayerID,
		WalletID:              body.WalletID,
		RoundID:               body.RoundID,
		GameID:                body.GameID,
		Kind:                  body.Kind,
		Amount:                body.Money.Amount,
		Currency:              body.Money.Currency,
		ReferenceExternalID:   body.ReferenceExternalID,
	}

	output, err := h.wagerUsecase.ProcessTransaction(r.Context(), input)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionConflict) {
			h.respondWithError(w, http.StatusConflict, "Transaction already processed")
			return
		}

		if errors.Is(err, domain.ErrIncompatibleCurrency) || errors.Is(err, domain.ErrInvalidMoneyFormat) {
			h.respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if output.IdempotentReplay {
		w.Header().Set("X-Cache-Lookup", "HIT - Idempotent Replay")
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(output)

}
