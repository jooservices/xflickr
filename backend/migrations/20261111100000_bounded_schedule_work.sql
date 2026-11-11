-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_failed_lease_expired_idx
    ON work_items (updated_at, id)
    WHERE state = 'failed' AND last_error = 'lease expired';

DROP INDEX CONCURRENTLY IF EXISTS work_items_failed_updated_idx;

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_failed_updated_idx
    ON work_items (updated_at)
    WHERE state = 'failed';

DROP INDEX CONCURRENTLY IF EXISTS work_items_failed_lease_expired_idx;
