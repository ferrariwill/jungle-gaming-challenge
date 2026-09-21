package database

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
)

func NewPostgresPool(lc fx.Lifecycle, cfg *config.Config) (*pgxpool.Pool, error) {
	ctx := context.Background()

	poolConfig, err := pgxpool.ParseConfig(cfg.DBConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database pool: %w", err)
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("failed to ping database: %w", err)
			}
			log.Println("Database connection established")
			if err := runMigrations(cfg.DBConnectionString); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}
			log.Println("Migrations applied successfully")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Println("Database connection closed")
			pool.Close()
			return nil
		},
	})
	return pool, nil
}

func runMigrations(dbURL string) error {
	log.Println("Running migrations...")

	m, err := migrate.New("file://migrations", dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	log.Println("Migrations applied successfully")
	return nil
}

func Ping(ctx context.Context, pool *pgxpool.Pool) error {
	return pool.Ping(ctx)
}
