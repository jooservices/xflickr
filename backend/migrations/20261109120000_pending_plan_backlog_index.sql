-- +goose Up
CREATE INDEX work_items_pending_plan_backlog_idx
    ON work_items (key_id, worker_type, user_id, not_before)
    WHERE state = 'pending';

-- +goose Down
DROP INDEX work_items_pending_plan_backlog_idx;
