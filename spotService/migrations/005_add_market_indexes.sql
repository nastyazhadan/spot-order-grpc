-- +goose Up
CREATE INDEX IF NOT EXISTS idx_market_store_visible_name_id
    ON market_store (name, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_market_store_enabled_visible_name_id
    ON market_store (name, id)
    WHERE deleted_at IS NULL AND enabled = TRUE;

-- +goose Down
DROP INDEX IF EXISTS idx_market_store_enabled_visible_name_id;
DROP INDEX IF EXISTS idx_market_store_visible_name_id;
