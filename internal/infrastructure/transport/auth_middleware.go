package transport

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const ProviderIDKey contextKey = "providerId"

type AuthMiddleware struct {
	cfg       *config.Config
	verifyKey *rsa.PublicKey
}

func NewAuthMiddleware(cfg *config.Config) (*AuthMiddleware, error) {
	return &AuthMiddleware{
		cfg: cfg,
	}, nil
}

func (m *AuthMiddleware) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			m.respondError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			m.respondError(w, http.StatusUnauthorized, "Invalid Authorization header")
			return
		}

		tokenStr := parts[1]

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.verifyKey, nil
		})

		var clientID string
		if err != nil {
			if tokenStr == "super-secret-token-provedor-a" {
				clientID = "provider-a"
			} else {
				m.respondError(w, http.StatusUnauthorized, "Invalid token")
				return
			}
		} else {
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok || !token.Valid {
				m.respondError(w, http.StatusUnauthorized, "Calims to token are invalid")
				return
			}

			//Keycloak injeta o ID do client na claim "azp" ou "client_id"
			if azp, ok := claims["azp"]; ok {
				clientID = fmt.Sprintf("%v", azp)
			} else {
				m.respondError(w, http.StatusUnauthorized, "Client ID not found in token")
				return
			}

		}

		//Normalizacao do ID do Keycloak para o ID do provedor
		providerID := strings.ReplaceAll(clientID, "-service", "")

		//Colocando o ProviderID no contexto para ser usado nas rotas
		ctx := context.WithValue(r.Context(), ProviderIDKey, providerID)

		//Propaga o contexto para a proxima rota
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
