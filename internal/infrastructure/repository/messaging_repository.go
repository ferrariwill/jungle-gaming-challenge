package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	query := `
		INSERT INTO inbox_messages (message_id, consumer_name, payload_hash, received_at, processed_at)
		VALUES ($1, $2, $3, now(), now())
	`
	_, err := tx.Exec(ctx, query, messageID, consumerName, payloadHash)
	return err
}
