-- +goose Up
CREATE TABLE IF NOT EXISTS outbox
(
    id           UUID PRIMARY KEY,
    event_id     UUID        NOT NULL,
    event_type   TEXT        NOT NULL,
    aggregate_id UUID        NOT NULL,
    payload      BYTEA       NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'pending',
    retry_count  INT         NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    failed_at    TIMESTAMPTZ,
    locked_at    TIMESTAMPTZ,
    last_error   TEXT,

    CONSTRAINT chk_outbox_status
        CHECK (status IN ('pending', 'processing', 'published', 'failed'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_event_id
    ON outbox (event_id);

CREATE INDEX IF NOT EXISTS idx_outbox_pending_available_at
    ON outbox (status, available_at, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_outbox_processing_locked_at
    ON outbox (locked_at)
    WHERE status = 'processing' AND locked_at IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS outbox;
