-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_contact_page_idx
    ON albums (owner_nsid, updated_at DESC NULLS LAST, id DESC);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS albums_contact_page_idx;
