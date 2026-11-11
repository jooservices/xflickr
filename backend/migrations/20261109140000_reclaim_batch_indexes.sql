-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS river_job_active_work_item_idx
    ON river_job ((args->'args'->>'work_item_id'))
    WHERE state IN ('available', 'pending', 'retryable', 'scheduled', 'running');

CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_active_key_count_idx
    ON work_items (key_id)
    WHERE state IN ('ready', 'running');

CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_active_worker_count_idx
    ON work_items (worker_type)
    WHERE state IN ('ready', 'running');

CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_pending_keyless_due_idx
    ON work_items (not_before)
    WHERE state = 'pending' AND key_id IS NULL;

INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('queue.reclaim_batch_size', '100'::jsonb, FALSE, 1, NULL)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM app_settings WHERE key = 'queue.reclaim_batch_size';
DROP INDEX CONCURRENTLY IF EXISTS work_items_pending_keyless_due_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_active_worker_count_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_active_key_count_idx;
DROP INDEX CONCURRENTLY IF EXISTS river_job_active_work_item_idx;
