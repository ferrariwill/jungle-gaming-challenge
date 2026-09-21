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
			usecase.NewWagerUsecase,
			usecase.NewOpenWalletUseCase,
			usecase.NewReconciliationUsecase,
			transport.NewAuthMiddleware,
			transport.NewHTTPHandler,
			transport.NewSQSWorker,
			transport.NewOutboxWorker,
			transport.NewPendingReferenceWorker,
		),
		fx.Invoke(
			StartHTTPServer,
			StartSQSWorker,
			StartOutboxWorker,
			StartPendingReferenceWorker,
		),
	)

	app.Run()
}

func StartHTTPServer(lifecycle fx.Lifecycle, handler *transport.HTTPHandler, shutdowner fx.Shutdowner) {
	server := &http.Server{
		Addr:    ":3000",
		Handler: handler,
	}

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Println("Starting HTTP server on port 3000")
			go func() {
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("HTTP server error: %v", err)
					_ = shutdowner.Shutdown()
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
			go worker.Start()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}

func StartPendingReferenceWorker(lifecycle fx.Lifecycle, worker *transport.PendingReferenceWorker) {
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
