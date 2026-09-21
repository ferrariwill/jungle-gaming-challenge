CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE wallets (
    id VARCHAR(64) PRIMARY KEY,
    player_id VARCHAR(64) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    amount BIGINT NOT NULL,
    version BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,

    CONSTRAINT chk_wallet_balance_not_negative CHECK (amount >= 0),
    CONSTRAINT uq_player_currency UNIQUE (player_id, currency),
    CONSTRAINT chk_wallet_version_positive CHECK (version > 0)
);

CREATE INDEX idx_wallets_player_id ON wallets(player_id);

CREATE TABLE wager_transactions (
    id VARCHAR(64) PRIMARY KEY,
    provider_id VARCHAR(64) NOT NULL,
    external_transaction_id VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    player_id VARCHAR(64) NOT NULL,
    round_id VARCHAR(64) NOT NULL,
    game_id VARCHAR(64) NOT NULL,
    kind VARCHAR(20) NOT NULL,
    amount BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,
    reference_external_id VARCHAR(64),
    status VARCHAR(30) NOT NULL,
    failure_code VARCHAR(50),
    next_retry_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,

    CONSTRAINT uq_provider_external_tx UNIQUE (provider_id, external_transaction_id),
    CONSTRAINT uq_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT chk_transaction_kind CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    CONSTRAINT chk_transaction_status CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED'))
);

CREATE INDEX idx_transactions_idempotency_key ON wager_transactions(idempotency_key);
CREATE INDEX idx_transactions_provider_ref ON wager_transactions(provider_id, reference_external_id);
CREATE INDEX idx_transactions_pending_reference ON wager_transactions(status, next_retry_at)
    WHERE status = 'PENDING_REFERENCE';

CREATE TABLE wallet_ledger_entries (
    id VARCHAR(64) PRIMARY KEY,
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    transaction_id VARCHAR(64) NOT NULL REFERENCES wager_transactions(id),
    direction VARCHAR(6) NOT NULL,
    amount BIGINT NOT NULL,
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,

    CONSTRAINT chk_ledger_direction CHECK (direction IN ('DEBIT', 'CREDIT')),
    CONSTRAINT uq_wallet_transaction_ledger UNIQUE (wallet_id, transaction_id),
    CONSTRAINT chk_ledger_math_consistency CHECK (
        (direction = 'CREDIT' AND balance_after = balance_before + amount) OR
        (direction = 'DEBIT' AND balance_after = balance_before - amount)
    ),
    CONSTRAINT chk_ledger_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_ledger_balance_not_negative CHECK (balance_after >= 0 AND balance_before >= 0)
);

CREATE TABLE inbox_messages (
    message_id VARCHAR(64) NOT NULL,
    consumer_name VARCHAR(100) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    received_at TIMESTAMP WITH TIME ZONE NOT NULL,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL,

    PRIMARY KEY (consumer_name, message_id)
);

CREATE TABLE outbox_events (
    id VARCHAR(64) PRIMARY KEY,
    aggregate_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload TEXT NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    next_send_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    published_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT chk_outbox_status CHECK (status IN ('PENDING', 'PUBLISHED', 'FAILED'))
);

CREATE INDEX idx_outbox_pending_polling ON outbox_events(status, next_send_at)
WHERE status = 'PENDING';

-- Trigger para proteger o ledger financeiro contra modificacoes
CREATE OR REPLACE FUNCTION block_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Operacao proibida: O ledger financeiro e estritamente append-only e imutavel.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_protect_ledger_entries
BEFORE UPDATE OR DELETE ON wallet_ledger_entries
FOR EACH ROW
EXECUTE FUNCTION block_ledger_mutation();
