-- +goose Up
ALTER TABLE crawl_interests
    ADD COLUMN last_checked_at TIMESTAMPTZ,
    ADD COLUMN last_seen_at TIMESTAMPTZ,
    ADD COLUMN last_fave_at TIMESTAMPTZ;

CREATE TABLE crawl_cycles (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    cursor_person_nsid TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE crawl_cycles;
ALTER TABLE crawl_interests
    DROP COLUMN last_fave_at,
    DROP COLUMN last_seen_at,
    DROP COLUMN last_checked_at;
