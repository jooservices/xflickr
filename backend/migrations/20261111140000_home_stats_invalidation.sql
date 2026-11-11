-- +goose Up
ALTER TABLE user_home_stats
    ADD COLUMN IF NOT EXISTS invalidated_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE user_home_stats
    DROP COLUMN IF EXISTS invalidated_at;
