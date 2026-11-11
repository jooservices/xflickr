-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX IF NOT EXISTS photos_owner_pending_sizes_idx ON ONLY photos (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';

CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p00_owner_pending_sizes_idx ON photos_p00 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p01_owner_pending_sizes_idx ON photos_p01 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p02_owner_pending_sizes_idx ON photos_p02 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p03_owner_pending_sizes_idx ON photos_p03 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p04_owner_pending_sizes_idx ON photos_p04 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p05_owner_pending_sizes_idx ON photos_p05 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p06_owner_pending_sizes_idx ON photos_p06 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p07_owner_pending_sizes_idx ON photos_p07 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p08_owner_pending_sizes_idx ON photos_p08 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p09_owner_pending_sizes_idx ON photos_p09 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p10_owner_pending_sizes_idx ON photos_p10 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p11_owner_pending_sizes_idx ON photos_p11 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p12_owner_pending_sizes_idx ON photos_p12 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p13_owner_pending_sizes_idx ON photos_p13 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p14_owner_pending_sizes_idx ON photos_p14 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p15_owner_pending_sizes_idx ON photos_p15 (owner_nsid, id)
    WHERE tombstoned_at IS NULL AND sizes_status <> 'confirmed';

-- +goose StatementBegin
DO $$
DECLARE
    partition_name TEXT;
    index_name TEXT;
    invalid_index TEXT;
BEGIN
    FOREACH partition_name IN ARRAY ARRAY[
        'photos_p00', 'photos_p01', 'photos_p02', 'photos_p03',
        'photos_p04', 'photos_p05', 'photos_p06', 'photos_p07',
        'photos_p08', 'photos_p09', 'photos_p10', 'photos_p11',
        'photos_p12', 'photos_p13', 'photos_p14', 'photos_p15'
    ] LOOP
        index_name := partition_name || '_owner_pending_sizes_idx';
        IF NOT EXISTS (
            SELECT 1 FROM pg_inherits
            WHERE inhparent = 'photos_owner_pending_sizes_idx'::regclass
              AND inhrelid = to_regclass(index_name)
        ) THEN
            EXECUTE format('ALTER INDEX photos_owner_pending_sizes_idx ATTACH PARTITION %I', index_name);
        END IF;
    END LOOP;

    SELECT c.relname INTO invalid_index
    FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
    WHERE c.relname ~ '^photos_p(0[0-9]|1[0-5])_owner_pending_sizes_idx$'
      AND NOT i.indisvalid
    LIMIT 1;
    IF invalid_index IS NOT NULL THEN
        RAISE EXCEPTION 'invalid concurrent index build %; drop it with DROP INDEX CONCURRENTLY and rerun this migration', invalid_index;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP INDEX photos_owner_pending_sizes_idx;
