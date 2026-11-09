-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_failed_updated_idx
    ON work_items (updated_at)
    WHERE state = 'failed';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS work_items_failed_updated_idx;
