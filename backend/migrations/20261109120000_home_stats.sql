-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE user_home_stats (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    captioned BIGINT NOT NULL DEFAULT 0 CHECK (captioned >= 0),
    computed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE user_home_contact_stats (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL REFERENCES people (nsid) ON DELETE CASCADE,
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    PRIMARY KEY (user_id, person_nsid)
);

CREATE TABLE user_home_stats_refreshes (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    cursor_record_id TEXT NOT NULL DEFAULT '',
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    captioned BIGINT NOT NULL DEFAULT 0 CHECK (captioned >= 0),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_home_contact_stats_refreshes (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL REFERENCES people (nsid) ON DELETE CASCADE,
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    PRIMARY KEY (user_id, person_nsid)
);

INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('home.stats_refresh_interval', '900'::jsonb, FALSE, 60, 86400)
ON CONFLICT (key) DO NOTHING;

CREATE INDEX observations_home_visibility_idx ON ONLY observations (record_kind, record_id, user_id);
CREATE INDEX CONCURRENTLY observations_p00_home_visibility_idx ON observations_p00 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p00_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p01_home_visibility_idx ON observations_p01 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p01_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p02_home_visibility_idx ON observations_p02 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p02_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p03_home_visibility_idx ON observations_p03 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p03_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p04_home_visibility_idx ON observations_p04 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p04_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p05_home_visibility_idx ON observations_p05 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p05_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p06_home_visibility_idx ON observations_p06 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p06_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p07_home_visibility_idx ON observations_p07 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p07_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p08_home_visibility_idx ON observations_p08 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p08_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p09_home_visibility_idx ON observations_p09 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p09_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p10_home_visibility_idx ON observations_p10 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p10_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p11_home_visibility_idx ON observations_p11 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p11_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p12_home_visibility_idx ON observations_p12 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p12_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p13_home_visibility_idx ON observations_p13 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p13_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p14_home_visibility_idx ON observations_p14 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p14_home_visibility_idx;
CREATE INDEX CONCURRENTLY observations_p15_home_visibility_idx ON observations_p15 (record_kind, record_id, user_id);
ALTER INDEX observations_home_visibility_idx ATTACH PARTITION observations_p15_home_visibility_idx;

CREATE INDEX photos_home_latest_idx ON ONLY photos (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p00_home_latest_idx ON photos_p00 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p00_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p01_home_latest_idx ON photos_p01 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p01_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p02_home_latest_idx ON photos_p02 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p02_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p03_home_latest_idx ON photos_p03 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p03_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p04_home_latest_idx ON photos_p04 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p04_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p05_home_latest_idx ON photos_p05 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p05_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p06_home_latest_idx ON photos_p06 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p06_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p07_home_latest_idx ON photos_p07 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p07_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p08_home_latest_idx ON photos_p08 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p08_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p09_home_latest_idx ON photos_p09 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p09_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p10_home_latest_idx ON photos_p10 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p10_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p11_home_latest_idx ON photos_p11 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p11_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p12_home_latest_idx ON photos_p12 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p12_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p13_home_latest_idx ON photos_p13 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p13_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p14_home_latest_idx ON photos_p14 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p14_home_latest_idx;
CREATE INDEX CONCURRENTLY photos_p15_home_latest_idx ON photos_p15 (uploaded_at DESC NULLS LAST, id DESC)
WHERE visibility = 'public' AND tombstoned_at IS NULL;
ALTER INDEX photos_home_latest_idx ATTACH PARTITION photos_p15_home_latest_idx;

-- +goose Down
DROP INDEX photos_home_latest_idx;
DROP INDEX observations_home_visibility_idx;
DROP TABLE user_home_contact_stats_refreshes;
DROP TABLE user_home_stats_refreshes;
DROP TABLE user_home_contact_stats;
DROP TABLE user_home_stats;
DELETE FROM app_settings WHERE key = 'home.stats_refresh_interval';
