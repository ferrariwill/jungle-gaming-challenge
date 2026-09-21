package transport

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const ProviderIDKey contextKey = "providerId"

type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Use string `json:"use"`
}

type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

type AuthMiddleware struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
	fetchedAt  time.Time
}

func NewAuthMiddleware(cfg *config.Config) (*AuthMiddleware, error) {
	m := &AuthMiddleware{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		keys: make(map[string]*rsa.PublicKey),
	}
	if cfg.IDPJWKSURL != "" {
		if err := m.refreshKeys(context.Background()); err != nil {
			// Allow boot before Keycloak is fully ready; keys refresh on demand.
			fmt.Printf("warning: initial JWKS fetch failed: %v\n", err)
		}
	}
	return m, nil
}

func (m *AuthMiddleware) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" || r.URL.Path == "/metrics" {
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
		providerID, err := m.verifyAndExtractProvider(r.Context(), tokenStr)
		if err != nil {
			m.respondError(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		ctx := context.WithValue(r.Context(), ProviderIDKey, providerID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *AuthMiddleware) verifyAndExtractProvider(ctx context.Context, tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		key, err := m.keyForKid(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	}, jwt.WithIssuer(m.cfg.IDPIssuerURL), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", errors.New("claims to token are invalid")
	}

	var clientID string
	if azp, ok := claims["azp"]; ok {
		clientID = fmt.Sprintf("%v", azp)
	} else if cid, ok := claims["client_id"]; ok {
		clientID = fmt.Sprintf("%v", cid)
	} else {
		return "", errors.New("client id not found in token")
	}

	return strings.ReplaceAll(clientID, "-service", ""), nil
}

func (m *AuthMiddleware) keyForKid(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	m.mu.RLock()
	key, ok := m.keys[kid]
	stale := time.Since(m.fetchedAt) > 5*time.Minute
	m.mu.RUnlock()
	if ok && !stale {
		return key, nil
	}

	if err := m.refreshKeys(ctx); err != nil && !ok {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok = m.keys[kid]
	if !ok {
		// fallback: any available key when kid missing
		if kid == "" {
			for _, k := range m.keys {
				return k, nil
			}
		}
		return nil, fmt.Errorf("jwks kid not found: %s", kid)
	}
	return key, nil
}

func (m *AuthMiddleware) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.cfg.IDPJWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks http status %d", resp.StatusCode)
	}

	var payload jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}

	next := make(map[string]*rsa.PublicKey)
	for _, k := range payload.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		next[k.Kid] = pub
	}
	if len(next) == 0 {
		return errors.New("no rsa keys in jwks")
	}

	m.mu.Lock()
	m.keys = next
	m.fetchedAt = time.Now()
	m.mu.Unlock()
	return nil
}

func rsaPublicKeyFromJWK(nStr, eStr string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nb)
	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, errors.New("invalid exponent")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}
