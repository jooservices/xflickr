-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY incidents_open_last_seen_id_idx
    ON incidents (last_seen DESC, id DESC)
    WHERE state = 'open';

-- +goose Down
DROP INDEX CONCURRENTLY incidents_open_last_seen_id_idx;
