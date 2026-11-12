-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY work_items_active_created_at_idx
    ON work_items (created_at)
    WHERE state IN ('pending', 'ready', 'running', 'waiting');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS work_items_active_created_at_idx;
