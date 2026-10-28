-- +goose Up
ALTER TABLE download_parts
    ADD COLUMN fetched_at TIMESTAMPTZ;

CREATE INDEX download_parts_unfetched_idx
    ON download_parts(download_id, part_number)
    WHERE fetched_at IS NULL;

-- +goose Down
DROP INDEX download_parts_unfetched_idx;
ALTER TABLE download_parts
    DROP COLUMN fetched_at;
