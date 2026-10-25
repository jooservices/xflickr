-- +goose Up
ALTER TABLE work_items
    ADD COLUMN direct_action BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN starred BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN depth INTEGER NOT NULL DEFAULT 0 CHECK (depth >= 0),
    ADD COLUMN score DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN score_updated_at TIMESTAMPTZ;

DROP INDEX IF EXISTS work_items_pending_score_idx;
CREATE INDEX work_items_pending_score_idx
    ON work_items (key_id, worker_type, score DESC, not_before, id)
    WHERE state = 'pending';

INSERT INTO app_settings (key, value, user_overridable, min) VALUES
    ('score_weight_plan', '10'::jsonb, FALSE, 0),
    ('score_weight_direct_action', '100'::jsonb, FALSE, 0),
    ('score_weight_starred', '20'::jsonb, FALSE, 0),
    ('score_weight_depth', '5'::jsonb, FALSE, 0),
    ('score_weight_age_minutes', '0.1'::jsonb, FALSE, 0),
    ('score_aging_batch_size', '500'::jsonb, FALSE, 1);

-- +goose Down
DELETE FROM app_settings WHERE key IN ('score_weight_plan', 'score_weight_direct_action', 'score_weight_starred', 'score_weight_depth', 'score_weight_age_minutes', 'score_aging_batch_size', 'score_max_depth');
DROP INDEX IF EXISTS work_items_pending_score_idx;
CREATE INDEX work_items_pending_score_idx
    ON work_items (key_id, worker_type, priority_base DESC, not_before, id)
    WHERE state = 'pending';
ALTER TABLE work_items DROP COLUMN score_updated_at, DROP COLUMN score, DROP COLUMN depth, DROP COLUMN starred, DROP COLUMN direct_action;
