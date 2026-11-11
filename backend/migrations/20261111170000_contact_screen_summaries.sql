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
    ON work_items (user_id, subject_key) INCLUDE (id)
    WHERE kind = 'photo_sizes' AND state IN ('pending', 'ready', 'running', 'waiting') AND direct_action = FALSE;

CREATE INDEX IF NOT EXISTS photos_owner_recent_idx ON ONLY photos (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX IF NOT EXISTS photos_owner_contact_id_idx ON ONLY photos (owner_nsid, id);
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
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p00_owner_contact_id_idx ON photos_p00 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p01_owner_contact_id_idx ON photos_p01 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p02_owner_contact_id_idx ON photos_p02 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p03_owner_contact_id_idx ON photos_p03 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p04_owner_contact_id_idx ON photos_p04 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p05_owner_contact_id_idx ON photos_p05 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p06_owner_contact_id_idx ON photos_p06 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p07_owner_contact_id_idx ON photos_p07 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p08_owner_contact_id_idx ON photos_p08 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p09_owner_contact_id_idx ON photos_p09 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p10_owner_contact_id_idx ON photos_p10 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p11_owner_contact_id_idx ON photos_p11 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p12_owner_contact_id_idx ON photos_p12 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p13_owner_contact_id_idx ON photos_p13 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p14_owner_contact_id_idx ON photos_p14 (owner_nsid, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS photos_p15_owner_contact_id_idx ON photos_p15 (owner_nsid, id);

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
        index_name := partition_name || '_owner_contact_id_idx';
        IF NOT EXISTS (
            SELECT 1 FROM pg_inherits
            WHERE inhparent = 'photos_owner_contact_id_idx'::regclass
              AND inhrelid = to_regclass(index_name)
        ) THEN
            EXECUTE format('ALTER INDEX photos_owner_contact_id_idx ATTACH PARTITION %I', index_name);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Backfill summaries from records attributed to each user in bounded ID pages.
-- Concurrent indexes keep page reads bounded without holding write locks.
-- +goose StatementBegin
CREATE OR REPLACE PROCEDURE backfill_contact_screen_summaries()
LANGUAGE plpgsql
AS $$
DECLARE
    last_user_id BIGINT := 0;
    last_person_nsid TEXT := '';
    contact_user_id BIGINT;
    contact_person_nsid TEXT;
    photo_after_id BIGINT;
    favorite_after_id BIGINT;
    page_last_id BIGINT;
    page_photo_count BIGINT;
    page_size_count BIGINT;
    page_tombstone_count BIGINT;
    page_favorite_count BIGINT;
    photos_total BIGINT;
    sizes_total BIGINT;
    tombstones_total BIGINT;
    favorites_total BIGINT;
BEGIN
    LOOP
        SELECT user_id, person_nsid INTO contact_user_id, contact_person_nsid
        FROM crawl_interests
        WHERE (user_id, person_nsid) > (last_user_id, last_person_nsid)
        ORDER BY user_id, person_nsid LIMIT 1;

        EXIT WHEN NOT FOUND;

        photos_total := 0;
        sizes_total := 0;
        tombstones_total := 0;
        favorites_total := 0;
        photo_after_id := 0;
        LOOP
            PERFORM 1 FROM crawl_interests
            WHERE user_id=contact_user_id AND person_nsid=contact_person_nsid
            FOR UPDATE;

            WITH photo_page AS MATERIALIZED (
                SELECT id, last_update, sizes_status, sizes_checked_at, tombstoned_at
                FROM photos
                WHERE owner_nsid=contact_person_nsid AND id > photo_after_id
                ORDER BY id LIMIT 500
            ), attributed AS MATERIALIZED (
                SELECT page.*,
                    EXISTS (
                        SELECT 1 FROM flickr_accounts fa
                        JOIN observations o ON o.user_id=fa.user_id AND o.flickr_account_id=fa.id
                        WHERE fa.user_id=contact_user_id AND o.record_kind='photo'
                          AND o.record_id=page.id::text
                    ) AS observed
                FROM photo_page page
            )
            SELECT max(id),
                count(*) FILTER (WHERE observed)::bigint,
                count(*) FILTER (WHERE observed AND sizes_status='confirmed'
                    AND sizes_checked_at >= last_update)::bigint,
                count(*) FILTER (WHERE tombstoned_at IS NOT NULL)::bigint
            INTO page_last_id, page_photo_count, page_size_count, page_tombstone_count
            FROM attributed;

            EXIT WHEN page_last_id IS NULL;

            photos_total := photos_total + page_photo_count;
            sizes_total := sizes_total + page_size_count;
            tombstones_total := tombstones_total + page_tombstone_count;
            UPDATE crawl_interests SET photos_crawled=GREATEST(photos_crawled,photos_total),
                sizes_completed=GREATEST(sizes_completed,sizes_total),
                tombstones_count=GREATEST(tombstones_count,tombstones_total)
            WHERE user_id=contact_user_id AND person_nsid=contact_person_nsid;
            photo_after_id := page_last_id;
            COMMIT;
        END LOOP;

        favorite_after_id := 0;
        LOOP
            PERFORM 1 FROM crawl_interests
            WHERE user_id=contact_user_id AND person_nsid=contact_person_nsid
            FOR UPDATE;

            WITH favorite_page AS MATERIALIZED (
                SELECT photo_id, removed_at
                FROM favorites
                WHERE person_nsid=contact_person_nsid AND photo_id > favorite_after_id
                ORDER BY photo_id LIMIT 500
            ), attributed AS MATERIALIZED (
                SELECT page.photo_id, page.removed_at, ph.tombstoned_at,
                    EXISTS (
                        SELECT 1 FROM flickr_accounts fa
                        JOIN observations o ON o.user_id=fa.user_id AND o.flickr_account_id=fa.id
                        WHERE fa.user_id=contact_user_id AND o.record_kind='photo'
                          AND o.record_id=page.photo_id::text
                    ) AS observed
                FROM favorite_page page
                JOIN photos ph ON ph.id=page.photo_id
            )
            SELECT max(photo_id), count(*) FILTER (WHERE removed_at IS NULL
                AND tombstoned_at IS NULL AND observed)::bigint
            INTO page_last_id, page_favorite_count
            FROM attributed;

            EXIT WHEN page_last_id IS NULL;

            favorites_total := favorites_total + page_favorite_count;
            UPDATE crawl_interests SET favorites_count=GREATEST(favorites_count,favorites_total)
            WHERE user_id=contact_user_id AND person_nsid=contact_person_nsid;
            favorite_after_id := page_last_id;
            COMMIT;
        END LOOP;

        last_user_id := contact_user_id;
        last_person_nsid := contact_person_nsid;
    END LOOP;
END;
$$;
-- +goose StatementEnd

CALL backfill_contact_screen_summaries();
DROP PROCEDURE backfill_contact_screen_summaries();

-- +goose Down
-- Keep the added enum value: PostgreSQL cannot remove enum labels without
-- rewriting every dependent row. Refuse rollback while such rows exist before
-- changing any other schema objects.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM downloads WHERE kind = 'favorites') THEN
        RAISE EXCEPTION 'cannot roll back contact summaries while favorites downloads exist';
    END IF;
END $$;
-- +goose StatementEnd
DROP INDEX photos_owner_contact_id_idx;
DROP INDEX photos_owner_recent_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_contact_photo_sizes_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_user_contact_state_idx;
DROP TABLE contact_group_crawl_preferences;
ALTER TABLE crawl_interests
    DROP COLUMN tombstones_count,
    DROP COLUMN sizes_completed,
    DROP COLUMN favorites_count,
    DROP COLUMN photos_crawled;
