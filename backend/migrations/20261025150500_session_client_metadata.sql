-- +goose Up
ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ip_address TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS user_agent;
