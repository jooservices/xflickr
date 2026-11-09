-- +goose NO TRANSACTION
-- +goose Up
ALTER TYPE download_kind ADD VALUE 'favorites';

ALTER TABLE crawl_interests
    ADD COLUMN photos_crawled BIGINT NOT NULL DEFAULT 0 CHECK (photos_crawled >= 0),
    ADD COLUMN favorites_count BIGINT NOT NULL DEFAULT 0 CHECK (favorites_count >= 0),
    ADD COLUMN sizes_completed BIGINT NOT NULL DEFAULT 0 CHECK (sizes_completed >= 0),
    ADD COLUMN tombstones_count BIGINT NOT NULL DEFAULT 0 CHECK (tombstones_count >= 0);

CREATE TABLE contact_group_crawl_preferences (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL,
    group_nsid TEXT NOT NULL REFERENCES groups(nsid) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, person_nsid, group_nsid),
    FOREIGN KEY (user_id, person_nsid) REFERENCES crawl_interests(user_id, person_nsid) ON DELETE CASCADE
);

CREATE INDEX work_items_user_contact_state_idx
    ON work_items (user_id, subject_key, updated_at DESC, id DESC) INCLUDE (state, kind)
    WHERE user_id IS NOT NULL;

CREATE INDEX photos_owner_recent_idx ON ONLY photos (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p00_owner_recent_idx ON photos_p00 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p01_owner_recent_idx ON photos_p01 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p02_owner_recent_idx ON photos_p02 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p03_owner_recent_idx ON photos_p03 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p04_owner_recent_idx ON photos_p04 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p05_owner_recent_idx ON photos_p05 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p06_owner_recent_idx ON photos_p06 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p07_owner_recent_idx ON photos_p07 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p08_owner_recent_idx ON photos_p08 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p09_owner_recent_idx ON photos_p09 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p10_owner_recent_idx ON photos_p10 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p11_owner_recent_idx ON photos_p11 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p12_owner_recent_idx ON photos_p12 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p13_owner_recent_idx ON photos_p13 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p14_owner_recent_idx ON photos_p14 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p15_owner_recent_idx ON photos_p15 (owner_nsid, last_update DESC, id DESC) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p00_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p01_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p02_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p03_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p04_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p05_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p06_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p07_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p08_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p09_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p10_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p11_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p12_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p13_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p14_owner_recent_idx;
ALTER INDEX photos_owner_recent_idx ATTACH PARTITION photos_p15_owner_recent_idx;

-- +goose Down
DROP INDEX photos_owner_recent_idx;
DROP INDEX work_items_user_contact_state_idx;
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
