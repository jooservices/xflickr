-- +goose Up
ALTER TABLE sessions
    ADD COLUMN user_agent TEXT NOT NULL DEFAULT '',
    ADD COLUMN ip_address TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions
    DROP COLUMN ip_address,
    DROP COLUMN user_agent;
