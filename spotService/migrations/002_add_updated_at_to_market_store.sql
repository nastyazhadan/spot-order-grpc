-- +goose Up
ALTER TABLE market_store
ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_market_store_updated_at_id
    ON market_store (updated_at, id);

CREATE OR REPLACE FUNCTION set_market_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_set_market_updated_at ON market_store;

CREATE TRIGGER trg_set_market_updated_at
BEFORE UPDATE ON market_store
FOR EACH ROW
EXECUTE FUNCTION set_market_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trg_set_market_updated_at ON market_store;
DROP FUNCTION IF EXISTS set_market_updated_at();

ALTER TABLE market_store
DROP COLUMN IF EXISTS updated_at;
