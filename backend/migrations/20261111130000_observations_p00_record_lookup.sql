-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '2s';
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p00_kind_record_idx
    ON observations_p00 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '2s';
DROP INDEX CONCURRENTLY IF EXISTS observations_p00_kind_record_idx;
RESET lock_timeout;
