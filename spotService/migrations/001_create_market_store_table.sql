-- +goose Up
CREATE TABLE IF NOT EXISTS market_store
(
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    deleted_at TIMESTAMPTZ
);

INSERT INTO market_store (id, name, enabled, deleted_at) VALUES
    (gen_random_uuid(), 'BTC-USDT', true,  NULL),
    (gen_random_uuid(), 'ETH-USDT', false, NULL),
    (gen_random_uuid(), 'DOGE-USDT', true, NOW()),
    (gen_random_uuid(), 'SOL-USDT', true,  NULL),
    (gen_random_uuid(), 'ADA-USDT', false, NULL);

-- +goose Down
DROP TABLE if EXISTS market_store;
