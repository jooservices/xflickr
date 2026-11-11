-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS sync_targets_user_id_id_idx
  ON sync_targets (user_id, id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS sync_targets_user_id_id_idx;
