-- +goose Up
CREATE TABLE rest_idempotency (
    principal text NOT NULL,
    key text NOT NULL CHECK (length(key) BETWEEN 1 AND 255),
    fingerprint text NOT NULL,
    response jsonb,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (principal, key)
);
CREATE INDEX rest_idempotency_expiry_idx ON rest_idempotency (expires_at);

-- +goose Down
DROP TABLE rest_idempotency;
