-- +goose Up
CREATE TABLE IF NOT EXISTS inbox
(
    id             UUID PRIMARY KEY,
    event_id       UUID        NOT NULL,
    topic          TEXT        NOT NULL,
    consumer_group TEXT        NOT NULL,
    payload        BYTEA       NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'processing',
    received_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at   TIMESTAMPTZ,
    failed_at      TIMESTAMPTZ,
    error_message  TEXT,

    CONSTRAINT chk_inbox_status
        CHECK (status IN ('processing', 'processed', 'failed'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_inbox_event_consumer_group
    ON inbox(event_id, consumer_group);

CREATE INDEX IF NOT EXISTS idx_inbox_status_received_at
    ON inbox(status, received_at);

-- +goose Down
DROP TABLE IF EXISTS inbox;