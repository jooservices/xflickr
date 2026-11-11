-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '2s';
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p01_kind_record_idx
    ON observations_p01 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p02_kind_record_idx
    ON observations_p02 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p03_kind_record_idx
    ON observations_p03 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p04_kind_record_idx
    ON observations_p04 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p05_kind_record_idx
    ON observations_p05 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p06_kind_record_idx
    ON observations_p06 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p07_kind_record_idx
    ON observations_p07 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p08_kind_record_idx
    ON observations_p08 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p09_kind_record_idx
    ON observations_p09 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p10_kind_record_idx
    ON observations_p10 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p11_kind_record_idx
    ON observations_p11 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p12_kind_record_idx
    ON observations_p12 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p13_kind_record_idx
    ON observations_p13 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p14_kind_record_idx
    ON observations_p14 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p15_kind_record_idx
    ON observations_p15 (record_kind, record_id)
    INCLUDE (user_id, flickr_account_id);
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '2s';
DROP INDEX CONCURRENTLY IF EXISTS observations_p01_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p02_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p03_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p04_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p05_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p06_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p07_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p08_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p09_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p10_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p11_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p12_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p13_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p14_kind_record_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p15_kind_record_idx;
RESET lock_timeout;
