package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrDuplicateMessage = errors.New("duplicate message")
	ErrNilTransaction   = errors.New("infrastructure: the provided SQL transaction is nil")
	ErrNilDatabasePool  = errors.New("infrastructure: database pool is nil")
)

type OutboxEventDTO struct {
	ID          string
	AggregateID string
	EventType   string
	Payload     string
	Status      string
	NextSendAt  time.Time
	CreatedAt   time.Time
}

type MessagingRepository struct {
	pool *pgxpool.Pool
}

func NewMessagingRepository(pool *pgxpool.Pool) *MessagingRepository {
	return &MessagingRepository{
		pool: pool,
	}
}

func (r *MessagingRepository) SaveOutbox(ctx context.Context, tx pgx.Tx, event OutboxEventDTO) error {
	if tx == nil {
		return ErrNilTransaction
	}
	query := `
		INSERT INTO outbox_events (id, aggregate_id, event_type, payload, status, next_send_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.Exec(ctx, query,
		event.ID,
		event.AggregateID,
		event.EventType,
		event.Payload,
		event.Status,
		event.NextSendAt,
		event.CreatedAt,
	)
	return err
}

func (r *MessagingRepository) SaveInbox(ctx context.Context, tx pgx.Tx, consumerName, messageID, payloadHash string) error {
	if tx == nil {
		return ErrNilTransaction
	}
	query := `
		INSERT INTO inbox_messages (message_id, consumer_name, payload_hash, received_at, processed_at)
		VALUES ($1, $2, $3, now(), now())
	`
	_, err := tx.Exec(ctx, query, messageID, consumerName, payloadHash)
	return err
}

func (r *MessagingRepository) FindPendingOutboxEvents(ctx context.Context, tx pgx.Tx, limit int) ([]OutboxEventDTO, error) {
	if tx == nil {
		return nil, ErrNilTransaction
	}

	query := `
		SELECT id, aggregate_id, event_type, payload, status, next_send_at, created_at
		FROM outbox_events
		WHERE status = 'PENDING' AND next_send_at <= $1
		ORDER BY next_send_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`
	now := time.Now().UTC()
	rows, err := tx.Query(ctx, query, now, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to find pending outbox events: %w", err)
	}
	defer rows.Close()

	var events []OutboxEventDTO
	for rows.Next() {
		var event OutboxEventDTO
		err := rows.Scan(&event.ID, &event.AggregateID, &event.EventType, &event.Payload,
			&event.Status, &event.NextSendAt, &event.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan outbox event: %w", err)
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *MessagingRepository) MarkOutboxAsPublished(ctx context.Context, tx pgx.Tx, id string) error {
	if tx == nil {
		return ErrNilTransaction
	}
	query := `
		UPDATE outbox_events
		SET status = 'PUBLISHED', published_at = $1
		WHERE id = $2
	`
	_, err := tx.Exec(ctx, query, time.Now().UTC(), id)

	if err != nil {
		return fmt.Errorf("failed to mark outbox as published: %w", err)
	}
	return nil
}

func (r *MessagingRepository) UpdateOutboxRetry(ctx context.Context, tx pgx.Tx, id string, nextSendAt time.Time) error {
	if tx == nil {
		return ErrNilTransaction
	}
	query := `
		UPDATE outbox_events
		SET attempts = attempts + 1, next_send_at = $1
		WHERE id = $2
	`
	_, err := tx.Exec(ctx, query, nextSendAt, id)
	if err != nil {
		return fmt.Errorf("failed to update outbox retry: %w", err)
	}
	return nil
}
