-- +goose Up
CREATE INDEX crawl_interests_user_starred_idx
    ON crawl_interests (user_id, person_nsid)
    WHERE starred;

CREATE INDEX work_items_user_subject_active_idx
    ON work_items (user_id, subject_key, state)
    WHERE user_id IS NOT NULL AND state IN ('pending', 'ready', 'running', 'waiting', 'failed');

-- +goose Down
DROP INDEX work_items_user_subject_active_idx;
DROP INDEX crawl_interests_user_starred_idx;
