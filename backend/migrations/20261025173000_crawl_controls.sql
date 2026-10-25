-- +goose Up
ALTER TABLE crawl_interests ADD COLUMN paused_at TIMESTAMPTZ;

CREATE INDEX crawl_interests_paused_idx ON crawl_interests (user_id, paused_at) WHERE paused_at IS NOT NULL;

-- +goose Down
DROP INDEX crawl_interests_paused_idx;
ALTER TABLE crawl_interests DROP COLUMN paused_at;
