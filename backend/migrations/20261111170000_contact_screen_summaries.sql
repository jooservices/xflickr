-- +goose NO TRANSACTION
-- +goose Up
ALTER TYPE download_kind ADD VALUE IF NOT EXISTS 'favorites';

ALTER TABLE crawl_interests
    ADD COLUMN IF NOT EXISTS photos_crawled BIGINT NOT NULL DEFAULT 0 CHECK (photos_crawled >= 0),
    ADD COLUMN IF NOT EXISTS favorites_count BIGINT NOT NULL DEFAULT 0 CHECK (favorites_count >= 0),
    ADD COLUMN IF NOT EXISTS sizes_completed BIGINT NOT NULL DEFAULT 0 CHECK (sizes_completed >= 0),
    ADD COLUMN IF NOT EXISTS tombstones_count BIGINT NOT NULL DEFAULT 0 CHECK (tombstones_count >= 0);

CREATE TABLE IF NOT EXISTS contact_group_crawl_preferences (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL,
    group_nsid TEXT NOT NULL REFERENCES groups(nsid) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, person_nsid, group_nsid),
    FOREIGN KEY (user_id, person_nsid) REFERENCES crawl_interests(user_id, person_nsid) ON DELETE CASCADE
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_user_contact_state_idx
    ON work_items (user_id, subject_key, updated_at DESC, id DESC) INCLUDE (state, kind)
    WHERE user_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS work_items_contact_photo_sizes_idx
    ON work_items (user_id, id) INCLUDE (subject_key)
    WHERE kind = 'photo_sizes' AND state IN ('pending', 'ready', 'running', 'waiting') AND direct_action = FALSE;

CREATE INDEX IF NOT EXISTS photos_owner_recent_idx ON ONLY photos (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p00_owner_recent_idx ON photos_p00 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p01_owner_recent_idx ON photos_p01 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p02_owner_recent_idx ON photos_p02 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p03_owner_recent_idx ON photos_p03 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p04_owner_recent_idx ON photos_p04 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p05_owner_recent_idx ON photos_p05 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p06_owner_recent_idx ON photos_p06 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p07_owner_recent_idx ON photos_p07 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p08_owner_recent_idx ON photos_p08 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p09_owner_recent_idx ON photos_p09 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p10_owner_recent_idx ON photos_p10 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p11_owner_recent_idx ON photos_p11 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p12_owner_recent_idx ON photos_p12 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p13_owner_recent_idx ON photos_p13 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p14_owner_recent_idx ON photos_p14 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p15_owner_recent_idx ON photos_p15 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;

-- +goose StatementBegin
DO $$
DECLARE
    partition_name TEXT;
    index_name TEXT;
BEGIN
    FOREACH partition_name IN ARRAY ARRAY[
        'photos_p00', 'photos_p01', 'photos_p02', 'photos_p03',
        'photos_p04', 'photos_p05', 'photos_p06', 'photos_p07',
        'photos_p08', 'photos_p09', 'photos_p10', 'photos_p11',
        'photos_p12', 'photos_p13', 'photos_p14', 'photos_p15'
    ] LOOP
        index_name := partition_name || '_owner_recent_idx';
        IF NOT EXISTS (
            SELECT 1 FROM pg_inherits
            WHERE inhparent = 'photos_owner_recent_idx'::regclass
              AND inhrelid = to_regclass(index_name)
        ) THEN
            EXECUTE format('ALTER INDEX photos_owner_recent_idx ATTACH PARTITION %I', index_name);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Backfill newly added summaries from records attributed to each user. Each
-- transaction handles at most 100 contacts so long migrations release locks.
-- +goose StatementBegin
CREATE OR REPLACE PROCEDURE backfill_contact_screen_summaries()
LANGUAGE plpgsql
AS $$
DECLARE
    last_user_id BIGINT := 0;
    last_person_nsid TEXT := '';
    batch_user_id BIGINT;
    batch_person_nsid TEXT;
BEGIN
    LOOP
        SELECT user_id, person_nsid INTO batch_user_id, batch_person_nsid
        FROM (
            SELECT user_id, person_nsid FROM crawl_interests
            WHERE (user_id, person_nsid) > (last_user_id, last_person_nsid)
            ORDER BY user_id, person_nsid LIMIT 100
        ) batch
        ORDER BY user_id DESC, person_nsid DESC LIMIT 1;

        EXIT WHEN NOT FOUND;

        WITH batch AS (
            SELECT user_id, person_nsid FROM crawl_interests
            WHERE (user_id, person_nsid) > (last_user_id, last_person_nsid)
              AND (user_id, person_nsid) <= (batch_user_id, batch_person_nsid)
        ), photo_counts AS (
            SELECT b.user_id, b.person_nsid,
                count(DISTINCT ph.id)::bigint AS photos_crawled,
                count(DISTINCT ph.id) FILTER (
                    WHERE ph.sizes_status = 'confirmed' AND ph.sizes_checked_at >= ph.last_update
                )::bigint AS sizes_completed
            FROM batch b
            JOIN photos ph ON ph.owner_nsid = b.person_nsid
            WHERE EXISTS (
                SELECT 1 FROM flickr_accounts fa
                JOIN observations o ON o.user_id = fa.user_id AND o.flickr_account_id = fa.id
                WHERE fa.user_id = b.user_id AND o.record_kind = 'photo' AND o.record_id = ph.id::text
            )
            GROUP BY b.user_id, b.person_nsid
        ), tombstone_counts AS (
            SELECT b.user_id, b.person_nsid, count(DISTINCT ph.id)::bigint AS tombstones_count
            FROM batch b
            JOIN photos ph ON ph.owner_nsid = b.person_nsid AND ph.tombstoned_at IS NOT NULL
            GROUP BY b.user_id, b.person_nsid
        ), favorite_counts AS (
            SELECT b.user_id, b.person_nsid, count(DISTINCT f.photo_id)::bigint AS favorites_count
            FROM batch b
            JOIN favorites f ON f.person_nsid = b.person_nsid AND f.removed_at IS NULL
            JOIN photos ph ON ph.id = f.photo_id AND ph.tombstoned_at IS NULL
            WHERE EXISTS (
                SELECT 1 FROM flickr_accounts fa
                JOIN observations o ON o.user_id = fa.user_id AND o.flickr_account_id = fa.id
                WHERE fa.user_id = b.user_id AND o.record_kind = 'photo' AND o.record_id = ph.id::text
            )
            GROUP BY b.user_id, b.person_nsid
        )
        UPDATE crawl_interests ci SET
            photos_crawled = COALESCE(pc.photos_crawled, 0),
            favorites_count = COALESCE(fc.favorites_count, 0),
            sizes_completed = COALESCE(pc.sizes_completed, 0),
            tombstones_count = COALESCE(tc.tombstones_count, 0)
        FROM batch b
        LEFT JOIN photo_counts pc USING (user_id, person_nsid)
        LEFT JOIN tombstone_counts tc USING (user_id, person_nsid)
        LEFT JOIN favorite_counts fc USING (user_id, person_nsid)
        WHERE ci.user_id = b.user_id AND ci.person_nsid = b.person_nsid;

        last_user_id := batch_user_id;
        last_person_nsid := batch_person_nsid;
        COMMIT;
    END LOOP;
END;
$$;
-- +goose StatementEnd

CALL backfill_contact_screen_summaries();
DROP PROCEDURE backfill_contact_screen_summaries();

-- +goose Down
DROP INDEX photos_owner_recent_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_contact_photo_sizes_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_user_contact_state_idx;
DROP TABLE contact_group_crawl_preferences;
ALTER TABLE crawl_interests
    DROP COLUMN tombstones_count,
    DROP COLUMN sizes_completed,
    DROP COLUMN favorites_count,
    DROP COLUMN photos_crawled;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM downloads WHERE kind = 'favorites') THEN
        RAISE EXCEPTION 'cannot remove favorites download kind while downloads of that kind exist';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE downloads ALTER COLUMN kind TYPE TEXT USING kind::text;
DROP TYPE download_kind;
CREATE TYPE download_kind AS ENUM ('contact', 'album', 'tray');
ALTER TABLE downloads ALTER COLUMN kind TYPE download_kind USING kind::download_kind;
