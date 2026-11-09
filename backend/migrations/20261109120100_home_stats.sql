-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS user_home_stats (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    captioned BIGINT NOT NULL DEFAULT 0 CHECK (captioned >= 0),
    computed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS user_home_contact_stats (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL REFERENCES people (nsid) ON DELETE CASCADE,
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    PRIMARY KEY (user_id, person_nsid)
);

CREATE TABLE IF NOT EXISTS user_home_stats_refreshes (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    cursor_record_id TEXT NOT NULL DEFAULT '',
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    captioned BIGINT NOT NULL DEFAULT 0 CHECK (captioned >= 0),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_home_contact_stats_refreshes (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL REFERENCES people (nsid) ON DELETE CASCADE,
    photos_total BIGINT NOT NULL DEFAULT 0 CHECK (photos_total >= 0),
    sizes_ready BIGINT NOT NULL DEFAULT 0 CHECK (sizes_ready >= 0),
    PRIMARY KEY (user_id, person_nsid)
);

INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('home.stats_refresh_interval', '900'::jsonb, FALSE, 60, 86400)
ON CONFLICT (key) DO NOTHING;

-- Build leaf indexes concurrently. Dropping first makes a failed, invalid index
-- safe to retry when Goose has not yet recorded the NO TRANSACTION migration.
DROP INDEX CONCURRENTLY IF EXISTS observations_p00_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p00_home_visibility_lookup_idx ON observations_p00 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p01_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p01_home_visibility_lookup_idx ON observations_p01 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p02_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p02_home_visibility_lookup_idx ON observations_p02 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p03_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p03_home_visibility_lookup_idx ON observations_p03 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p04_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p04_home_visibility_lookup_idx ON observations_p04 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p05_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p05_home_visibility_lookup_idx ON observations_p05 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p06_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p06_home_visibility_lookup_idx ON observations_p06 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p07_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p07_home_visibility_lookup_idx ON observations_p07 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p08_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p08_home_visibility_lookup_idx ON observations_p08 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p09_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p09_home_visibility_lookup_idx ON observations_p09 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p10_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p10_home_visibility_lookup_idx ON observations_p10 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p11_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p11_home_visibility_lookup_idx ON observations_p11 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p12_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p12_home_visibility_lookup_idx ON observations_p12 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p13_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p13_home_visibility_lookup_idx ON observations_p13 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p14_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p14_home_visibility_lookup_idx ON observations_p14 (record_kind, record_id, user_id);
DROP INDEX CONCURRENTLY IF EXISTS observations_p15_home_visibility_lookup_idx;
CREATE INDEX CONCURRENTLY observations_p15_home_visibility_lookup_idx ON observations_p15 (record_kind, record_id, user_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS observations_p00_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p01_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p02_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p03_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p04_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p05_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p06_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p07_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p08_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p09_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p10_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p11_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p12_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p13_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p14_home_visibility_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS observations_p15_home_visibility_lookup_idx;
DROP TABLE IF EXISTS user_home_contact_stats_refreshes;
DROP TABLE IF EXISTS user_home_stats_refreshes;
DROP TABLE IF EXISTS user_home_contact_stats;
DROP TABLE IF EXISTS user_home_stats;
DELETE FROM app_settings WHERE key = 'home.stats_refresh_interval';
