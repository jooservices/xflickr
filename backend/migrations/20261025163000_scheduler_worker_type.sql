-- +goose Up
ALTER TABLE work_items
    ADD COLUMN worker_type TEXT GENERATED ALWAYS AS (
        CASE
            WHEN kind LIKE 'ai%' THEN 'ai'
            WHEN kind LIKE 'media%' THEN 'media'
            ELSE 'sync'
        END
    ) STORED;

DROP INDEX IF EXISTS work_items_pending_score_idx;
CREATE INDEX work_items_pending_score_idx
    ON work_items (key_id, worker_type, priority_base DESC, not_before, id)
    WHERE state = 'pending';

DROP INDEX IF EXISTS work_items_ready_lease_idx;
CREATE INDEX work_items_ready_lease_idx
    ON work_items (lease_expires_at, id)
    WHERE state IN ('ready', 'running');

-- +goose Down
DROP INDEX IF EXISTS work_items_ready_lease_idx;
CREATE INDEX work_items_ready_lease_idx ON work_items (lease_expires_at, id)
    WHERE state = 'ready';
DROP INDEX IF EXISTS work_items_pending_score_idx;
CREATE INDEX work_items_pending_score_idx ON work_items (key_id, priority_base DESC, not_before, id)
    WHERE state = 'pending';
ALTER TABLE work_items DROP COLUMN worker_type;
