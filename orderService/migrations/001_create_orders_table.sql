-- +goose Up
CREATE TABLE IF NOT EXISTS orders
(
    id         UUID           PRIMARY KEY,
    user_id    UUID           NOT NULL,
    market_id  UUID           NOT NULL,
    type       SMALLINT       NOT NULL,
    price      NUMERIC(18, 8) NOT NULL,
    quantity   BIGINT         NOT NULL,
    status     SMALLINT       NOT NULL,
    created_at TIMESTAMPTZ    NOT NULL,

    CONSTRAINT chk_orders_price_positive CHECK (price > 0),
    CONSTRAINT chk_orders_quantity_positive CHECK (quantity > 0),

    CONSTRAINT chk_orders_type_valid CHECK (type BETWEEN 0 AND 4),
    CONSTRAINT chk_orders_status_valid CHECK (status BETWEEN 0 AND 4)
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_market_id ON orders(market_id);

-- +goose Down
DROP TABLE if EXISTS orders;
