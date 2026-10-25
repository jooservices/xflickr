-- +goose Up
-- Per-key hourly quota usage flushed from Redis every minute so a Redis loss
-- cannot allow overspending. window_start matches the Redis window start.
CREATE TABLE quota_usage (
    key_id BIGINT NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    used BIGINT NOT NULL CHECK (used >= 0),
    by_class JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (key_id, window_start)
);

-- +goose Down
DROP TABLE quota_usage;
