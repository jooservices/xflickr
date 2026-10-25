-- +goose Up
-- Crawl producers inserted work_items without key_id, so the pump never saw them.
UPDATE work_items w SET key_id = owner.key_id
FROM (SELECT DISTINCT ON (user_id) user_id, key_id FROM flickr_accounts WHERE status = 'ok' ORDER BY user_id, id) owner
WHERE w.key_id IS NULL AND w.state IN ('pending', 'ready', 'running') AND w.user_id = owner.user_id;

-- +goose Down
SELECT 1;
