-- +goose Up
CREATE TABLE IF NOT EXISTS market_poller_cursor (
    poller_name  TEXT PRIMARY KEY,
    last_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_id UUID NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS market_poller_cursor;
DROP INDEX IF EXISTS idx_market_store_updated_at_id;
