-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY nodes_duplicate_egress_idx ON nodes (egress_ip, name)
WHERE state = 'online' AND roles @> ARRAY['crawl']::text[] AND egress_ip IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS nodes_duplicate_egress_idx;
