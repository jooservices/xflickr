-- +goose Up
ALTER TABLE work_items ADD COLUMN lease_expires_at TIMESTAMPTZ;

DROP INDEX IF EXISTS work_items_ready_idx;
CREATE INDEX work_items_pending_score_idx ON work_items (key_id, priority_base DESC, not_before, id)
    WHERE state = 'pending';
CREATE INDEX work_items_ready_lease_idx ON work_items (lease_expires_at, id)
    WHERE state = 'ready';

INSERT INTO app_settings (key, value, user_overridable, min, max) VALUES
    ('per_key_ready_cap', '100'::jsonb, FALSE, 0, NULL),
    ('node_ready_cap', '500'::jsonb, FALSE, 0, NULL),
    ('media_ready_cap', '100'::jsonb, FALSE, 0, NULL),
    ('ai_ready_cap', '100'::jsonb, FALSE, 0, NULL),
    ('sync_ready_cap', '100'::jsonb, FALSE, 0, NULL),
    ('lease_timeout', '120'::jsonb, FALSE, 1, NULL)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM app_settings WHERE key IN ('per_key_ready_cap', 'node_ready_cap', 'media_ready_cap', 'ai_ready_cap', 'sync_ready_cap', 'lease_timeout');
DROP INDEX IF EXISTS work_items_ready_lease_idx;
DROP INDEX IF EXISTS work_items_pending_score_idx;
CREATE INDEX work_items_ready_idx ON work_items (priority_base DESC, not_before, id)
    WHERE state = 'ready';
ALTER TABLE work_items DROP COLUMN lease_expires_at;
