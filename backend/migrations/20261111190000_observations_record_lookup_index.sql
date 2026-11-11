-- +goose NO TRANSACTION
-- Build leaf indexes independently so the populated observations table remains
-- writable. CREATE/DROP INDEX CONCURRENTLY cannot target a partitioned parent.

-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p01_record_lookup_idx ON observations_p01 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p02_record_lookup_idx ON observations_p02 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p03_record_lookup_idx ON observations_p03 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p04_record_lookup_idx ON observations_p04 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p05_record_lookup_idx ON observations_p05 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p06_record_lookup_idx ON observations_p06 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p07_record_lookup_idx ON observations_p07 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p08_record_lookup_idx ON observations_p08 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p09_record_lookup_idx ON observations_p09 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p10_record_lookup_idx ON observations_p10 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p11_record_lookup_idx ON observations_p11 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p12_record_lookup_idx ON observations_p12 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p13_record_lookup_idx ON observations_p13 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p14_record_lookup_idx ON observations_p14 (record_id, record_kind, user_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p15_record_lookup_idx ON observations_p15 (record_id, record_kind, user_id);

-- A failed concurrent build leaves an invalid index. Do not let IF NOT EXISTS
-- silently accept it on retry and mark this migration complete.
-- +goose StatementBegin
DO $$
DECLARE
    invalid_index TEXT;
BEGIN
    SELECT c.relname
    INTO invalid_index
    FROM pg_index i
    JOIN pg_class c ON c.oid = i.indexrelid
    WHERE c.relname ~ '^observations_p(0[1-9]|1[0-5])_record_lookup_idx$'
      AND NOT i.indisvalid
    LIMIT 1;

    IF invalid_index IS NOT NULL THEN
        RAISE EXCEPTION 'invalid concurrent index build %; drop it with DROP INDEX CONCURRENTLY and rerun this migration', invalid_index;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS observations_p01_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p02_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p03_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p04_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p05_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p06_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p07_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p08_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p09_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p10_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p11_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p12_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p13_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p14_record_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p15_record_lookup_idx;
