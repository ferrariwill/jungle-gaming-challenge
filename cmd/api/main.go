package main

import (
	"context"
	"log"
	"net/http"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/transport"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
	"go.uber.org/fx"
)

func main() {

	app := fx.New(
		fx.Provide(
			config.NewConfig,
			database.NewPostgresPool,
			database.NewTransactionManager,
			repository.NewWalletRepository,
			repository.NewTransactionRepository,
			repository.NewMessagingRepository,
			usecase.NewWagerUseCase,
			usecase.NewOpenWalletUseCase,
			transport.NewAuthMiddleware,
			transport.NewHTTPHandler,
		),
		fx.Invoke(StartHTTPServer),
	)

	app.Run()
}

func StartHTTPServer(lifecycle fx.Lifecycle, handler *transport.HTTPHandler) {
	server := &http.Server{
		Addr:    ":3000",
		Handler: handler,
	}

	lifecycle.Append(
		fx.Hook{
			OnStart: func(ctx context.Context) error {
				log.Println("Starting HTTP server on port 3000")
				go func() {
					if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						log.Fatalf("Failed to start HTTP server: %v", err)
					}
				}()
				return nil
			},
			OnStop: func(ctx context.Context) error {
				log.Println("Stopping HTTP server...")
				return server.Shutdown(ctx)
			},
		})

}
