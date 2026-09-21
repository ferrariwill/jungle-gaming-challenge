package database_test

import (
	"testing"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"go.uber.org/fx"
)

type mockLifecycle struct {
	fx.Lifecycle
}

func (m *mockLifecycle) Append(hook fx.Hook) {}

func TestNewPostgresPool_RejectsInvalidConnectionString(t *testing.T) {
	lc := &mockLifecycle{}

	cfg := &config.Config{
		DBConnectionString: "completely-invalid-connection-string",
	}

	_, err := database.NewPostgresPool(lc, cfg)
	if err == nil {
		t.Fatal("expected error to parse an invalid connection string, but got nil")
	}
}
