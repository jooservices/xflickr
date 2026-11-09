-- +goose Up
CREATE INDEX IF NOT EXISTS work_items_failed_updated_idx
    ON work_items (updated_at)
    WHERE state = 'failed';

-- +goose Down
DROP INDEX IF EXISTS work_items_failed_updated_idx;
