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
			usecase.NewReconciliationUsecase,
			transport.NewAuthMiddleware,
			transport.NewHTTPHandler,
			transport.NewSQSWorker,
			transport.NewOutboxWorker,
		),
		fx.Invoke(
			StartHTTPServer,
			StartSQSWorker,
			StartOutboxWorker),
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

func StartSQSWorker(lifecycle fx.Lifecycle, worker *transport.SQSWorker) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go worker.Start()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}

func StartOutboxWorker(lifecycle fx.Lifecycle, worker *transport.OutboxWorker) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go worker.Start() // Inicia o loop assíncrono em background
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx) // Garante Graceful Shutdown sob sinais do SO
		},
	})
}
