-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS user_home_photo_candidates (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    uploaded_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, photo_id)
);

CREATE TABLE IF NOT EXISTS user_home_shared_photo_candidates (
    photo_id BIGINT PRIMARY KEY REFERENCES photos (id) ON DELETE CASCADE,
    uploaded_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS user_home_sharing_syncs (
    user_id BIGINT PRIMARY KEY REFERENCES sharing_settings (user_id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL,
    cursor_photo_id BIGINT NOT NULL DEFAULT 0
);

DROP INDEX CONCURRENTLY IF EXISTS user_home_photo_candidates_recent_idx;
CREATE INDEX CONCURRENTLY user_home_photo_candidates_recent_idx
    ON user_home_photo_candidates (user_id, uploaded_at DESC NULLS LAST, photo_id DESC);
DROP INDEX CONCURRENTLY IF EXISTS user_home_photo_candidates_photo_idx;
CREATE INDEX CONCURRENTLY user_home_photo_candidates_photo_idx
    ON user_home_photo_candidates (photo_id);
DROP INDEX CONCURRENTLY IF EXISTS user_home_shared_photo_candidates_recent_idx;
CREATE INDEX CONCURRENTLY user_home_shared_photo_candidates_recent_idx
    ON user_home_shared_photo_candidates (uploaded_at DESC NULLS LAST, photo_id DESC);
DROP INDEX CONCURRENTLY IF EXISTS sharing_settings_enabled_home_idx;
CREATE INDEX CONCURRENTLY sharing_settings_enabled_home_idx
    ON sharing_settings (user_id) WHERE enabled;
DROP INDEX CONCURRENTLY IF EXISTS work_items_user_activity_idx;
CREATE INDEX CONCURRENTLY work_items_user_activity_idx
    ON work_items (user_id, updated_at DESC, id DESC)
    WHERE user_id IS NOT NULL
      AND kind IN ('photos', 'sizes')
      AND state IN ('ready', 'running', 'waiting', 'failed', 'done');
DROP INDEX CONCURRENTLY IF EXISTS work_items_home_contact_state_idx;
CREATE INDEX CONCURRENTLY work_items_home_contact_state_idx
    ON work_items (user_id, subject_key, state)
    WHERE state IN ('pending', 'ready', 'running', 'waiting', 'failed');
DROP INDEX CONCURRENTLY IF EXISTS downloads_user_activity_idx;
CREATE INDEX CONCURRENTLY downloads_user_activity_idx
    ON downloads (user_id, created_at DESC, id DESC);

ANALYZE user_home_photo_candidates;
ANALYZE user_home_shared_photo_candidates;
ANALYZE user_home_sharing_syncs;
ANALYZE sharing_settings;

CREATE TABLE IF NOT EXISTS user_home_stats_versions (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0)
);
INSERT INTO user_home_stats_versions (user_id)
SELECT id FROM users ON CONFLICT (user_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS user_home_shared_version (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0)
);
INSERT INTO user_home_shared_version (id) VALUES (TRUE) ON CONFLICT (id) DO NOTHING;
ANALYZE user_home_shared_version;

ALTER TABLE user_home_stats
    ADD COLUMN IF NOT EXISTS source_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS shared_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE user_home_stats_refreshes
    ADD COLUMN IF NOT EXISTS source_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS shared_revision BIGINT NOT NULL DEFAULT 0;

INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('home.stats_refresh_batch_size', '250'::jsonb, FALSE, 250, 1000)
ON CONFLICT (key) DO UPDATE
SET value = to_jsonb(LEAST(GREATEST((app_settings.value #>> '{}')::bigint, 250), 1000)),
    min = 250,
    max = 1000;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sync_home_shared_photo_candidate(affected_photo BIGINT) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
    SELECT photo.id, photo.uploaded_at
    FROM photos photo
    WHERE photo.id = affected_photo
      AND photo.visibility = 'public'
      AND photo.tombstoned_at IS NULL
      AND EXISTS (
          SELECT 1
          FROM observations observation
          JOIN sharing_settings setting ON setting.user_id = observation.user_id AND setting.enabled
          WHERE observation.record_kind = 'photo'
            AND observation.record_id = photo.id::text
      )
    ON CONFLICT (photo_id) DO UPDATE SET uploaded_at = EXCLUDED.uploaded_at;

    DELETE FROM user_home_shared_photo_candidates candidate
    WHERE candidate.photo_id = affected_photo
      AND NOT EXISTS (
          SELECT 1
          FROM photos photo
          WHERE photo.id = affected_photo
            AND photo.visibility = 'public'
            AND photo.tombstoned_at IS NULL
            AND EXISTS (
                SELECT 1
                FROM observations observation
                JOIN sharing_settings setting ON setting.user_id = observation.user_id AND setting.enabled
                WHERE observation.record_kind = 'photo'
                  AND observation.record_id = photo.id::text
            )
      );
END;
$$;

CREATE OR REPLACE FUNCTION sync_home_observation_candidate() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_user BIGINT;
    affected_photo BIGINT;
BEGIN
    IF TG_OP <> 'INSERT' AND OLD.record_kind = 'photo' AND OLD.record_id ~ '^[0-9]+$' THEN
        affected_user := OLD.user_id;
        affected_photo := OLD.record_id::bigint;
        DELETE FROM user_home_photo_candidates candidate
        WHERE candidate.user_id = affected_user AND candidate.photo_id = affected_photo
          AND NOT EXISTS (
              SELECT 1 FROM observations remaining
              WHERE remaining.user_id = affected_user
                AND remaining.record_kind = 'photo'
                AND remaining.record_id = affected_photo::text
          );
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.record_kind = 'photo' AND NEW.record_id ~ '^[0-9]+$' THEN
        INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
        SELECT NEW.user_id, photo.id, photo.uploaded_at
        FROM photos photo
        WHERE photo.id = NEW.record_id::bigint
          AND photo.visibility = 'public'
          AND photo.tombstoned_at IS NULL
        ON CONFLICT (user_id, photo_id) DO UPDATE
        SET uploaded_at = EXCLUDED.uploaded_at;
    END IF;
    IF TG_OP <> 'INSERT' AND OLD.record_kind = 'photo' AND OLD.record_id ~ '^[0-9]+$'
       AND (TG_OP = 'DELETE' OR OLD.user_id IS DISTINCT FROM NEW.user_id
            OR OLD.record_kind IS DISTINCT FROM NEW.record_kind OR OLD.record_id IS DISTINCT FROM NEW.record_id) THEN
        PERFORM sync_home_shared_photo_candidate(OLD.record_id::bigint);
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.record_kind = 'photo' AND NEW.record_id ~ '^[0-9]+$'
       AND (TG_OP = 'INSERT' OR OLD.user_id IS DISTINCT FROM NEW.user_id
            OR OLD.record_kind IS DISTINCT FROM NEW.record_kind OR OLD.record_id IS DISTINCT FROM NEW.record_id) THEN
        PERFORM sync_home_shared_photo_candidate(NEW.record_id::bigint);
    END IF;

    IF TG_OP <> 'INSERT' THEN
        PERFORM bump_home_user_revision(OLD.user_id);
    END IF;
    IF TG_OP <> 'DELETE' AND (TG_OP = 'INSERT' OR NEW.user_id <> OLD.user_id) THEN
        PERFORM bump_home_user_revision(NEW.user_id);
    ELSIF TG_OP = 'UPDATE' AND OLD.record_kind IS DISTINCT FROM NEW.record_kind THEN
        PERFORM bump_home_user_revision(NEW.user_id);
    ELSIF TG_OP = 'UPDATE' AND OLD.record_id IS DISTINCT FROM NEW.record_id AND NEW.user_id = OLD.user_id THEN
        PERFORM bump_home_user_revision(NEW.user_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION sync_home_photo_candidates() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_photo BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM user_home_photo_candidates WHERE photo_id = OLD.id;
    ELSIF TG_OP = 'INSERT' THEN
        INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
        SELECT DISTINCT observation.user_id, NEW.id, NEW.uploaded_at
        FROM observations observation
        WHERE observation.record_kind = 'photo'
          AND observation.record_id = NEW.id::text
          AND NEW.visibility = 'public'
          AND NEW.tombstoned_at IS NULL
        ON CONFLICT (user_id, photo_id) DO UPDATE
        SET uploaded_at = EXCLUDED.uploaded_at;
    ELSIF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.visibility IS DISTINCT FROM NEW.visibility
       OR OLD.tombstoned_at IS DISTINCT FROM NEW.tombstoned_at THEN
        DELETE FROM user_home_photo_candidates WHERE photo_id = OLD.id;
        INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
        SELECT DISTINCT observation.user_id, NEW.id, NEW.uploaded_at
        FROM observations observation
        WHERE observation.record_kind = 'photo'
          AND observation.record_id = NEW.id::text
          AND NEW.visibility = 'public'
          AND NEW.tombstoned_at IS NULL
        ON CONFLICT (user_id, photo_id) DO UPDATE
        SET uploaded_at = EXCLUDED.uploaded_at;
    ELSIF OLD.uploaded_at IS DISTINCT FROM NEW.uploaded_at THEN
        UPDATE user_home_photo_candidates
        SET uploaded_at = NEW.uploaded_at
        WHERE photo_id = NEW.id;
    END IF;

    IF TG_OP = 'DELETE' THEN
        affected_photo := OLD.id;
        PERFORM sync_home_shared_photo_candidate(affected_photo);
    ELSE
        affected_photo := NEW.id;
        PERFORM sync_home_shared_photo_candidate(affected_photo);
    END IF;
    PERFORM bump_home_photo_revisions(affected_photo);
    IF TG_OP = 'UPDATE' AND OLD.id <> NEW.id THEN
        PERFORM sync_home_shared_photo_candidate(OLD.id);
        PERFORM bump_home_photo_revisions(OLD.id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION sync_home_enrichment_revision() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.kind = 'caption' THEN
            PERFORM bump_home_photo_revisions(OLD.photo_id);
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.kind = 'caption' THEN
            PERFORM bump_home_photo_revisions(NEW.photo_id);
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.kind = 'caption'
       AND (NEW.kind <> 'caption' OR OLD.photo_id <> NEW.photo_id) THEN
        PERFORM bump_home_photo_revisions(OLD.photo_id);
    END IF;
    IF NEW.kind = 'caption' THEN
        PERFORM bump_home_photo_revisions(NEW.photo_id);
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION bump_home_user_revision(affected_user BIGINT) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = affected_user) THEN
        RETURN;
    END IF;
    INSERT INTO user_home_stats_versions (user_id, revision)
    VALUES (affected_user, 2)
    ON CONFLICT (user_id) DO UPDATE
    SET revision = user_home_stats_versions.revision + 1;
    IF EXISTS (SELECT 1 FROM sharing_settings WHERE user_id = affected_user AND enabled) THEN
        UPDATE user_home_shared_version SET revision = revision + 1 WHERE id = TRUE;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION bump_home_photo_revisions(affected_photo BIGINT) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO user_home_stats_versions (user_id, revision)
    SELECT DISTINCT observation.user_id, 2
    FROM observations observation
    JOIN users account ON account.id = observation.user_id
    WHERE observation.record_kind = 'photo' AND observation.record_id = affected_photo::text
    ON CONFLICT (user_id) DO UPDATE
    SET revision = user_home_stats_versions.revision + 1;
    IF EXISTS (
        SELECT 1
        FROM observations observation
        JOIN sharing_settings setting ON setting.user_id = observation.user_id AND setting.enabled
        WHERE observation.record_kind = 'photo' AND observation.record_id = affected_photo::text
    ) THEN
        UPDATE user_home_shared_version SET revision = revision + 1 WHERE id = TRUE;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION bump_home_sharing_revision() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    changed BOOLEAN;
    should_sync BOOLEAN;
BEGIN
    IF TG_OP = 'INSERT' THEN
        changed := TRUE;
        should_sync := NEW.enabled;
    ELSE
        changed := OLD.enabled IS DISTINCT FROM NEW.enabled;
        should_sync := changed;
    END IF;

    IF changed THEN
        INSERT INTO user_home_stats_versions (user_id, revision)
        VALUES (NEW.user_id, 2)
        ON CONFLICT (user_id) DO UPDATE
        SET revision = user_home_stats_versions.revision + 1;
    END IF;
    IF should_sync THEN
        INSERT INTO user_home_sharing_syncs (user_id, enabled, cursor_photo_id)
        VALUES (NEW.user_id, NEW.enabled, 0)
        ON CONFLICT (user_id) DO UPDATE
        SET enabled = EXCLUDED.enabled,
            cursor_photo_id = 0;
        UPDATE user_home_shared_version SET revision = revision + 1 WHERE id = TRUE;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS user_home_candidates_backfill_progress (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    cursor_photo_id BIGINT NOT NULL DEFAULT 0
);
INSERT INTO user_home_candidates_backfill_progress (id, cursor_photo_id)
VALUES (TRUE, 0) ON CONFLICT (id) DO NOTHING;

-- +goose StatementBegin
CREATE OR REPLACE PROCEDURE backfill_home_photo_candidates(batch_size INTEGER)
LANGUAGE plpgsql AS $$
DECLARE
    last_photo_id BIGINT;
    next_photo_id BIGINT;
    page_count INTEGER;
BEGIN
    IF batch_size < 1 OR batch_size > 1000 THEN
        RAISE EXCEPTION 'home candidate backfill batch size is outside the supported range';
    END IF;

    CREATE TEMP TABLE IF NOT EXISTS home_photo_candidate_page (
        photo_id BIGINT PRIMARY KEY
    ) ON COMMIT DELETE ROWS;

    LOOP
        SELECT cursor_photo_id INTO last_photo_id
        FROM user_home_candidates_backfill_progress
        WHERE id = TRUE
        FOR UPDATE;

        TRUNCATE pg_temp.home_photo_candidate_page;
        INSERT INTO pg_temp.home_photo_candidate_page (photo_id)
        SELECT photo.id
        FROM photos photo
        WHERE photo.id > last_photo_id
        ORDER BY photo.id
        LIMIT batch_size;
        GET DIAGNOSTICS page_count = ROW_COUNT;
        IF page_count = 0 THEN
            EXIT;
        END IF;

        SELECT max(photo_id) INTO next_photo_id
        FROM pg_temp.home_photo_candidate_page;

        INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
        SELECT DISTINCT observed.user_id, observed.photo_id, observed.uploaded_at
        FROM (
            SELECT observation.user_id, photo.id AS photo_id, photo.uploaded_at
            FROM pg_temp.home_photo_candidate_page page
            JOIN photos photo ON photo.id = page.photo_id
            JOIN observations observation
              ON observation.record_kind = 'photo'
             AND observation.record_id = photo.id::text
            WHERE photo.visibility = 'public'
              AND photo.tombstoned_at IS NULL
            FOR UPDATE OF observation
        ) observed
        ON CONFLICT (user_id, photo_id) DO UPDATE
        SET uploaded_at = EXCLUDED.uploaded_at;

        DELETE FROM user_home_photo_candidates candidate
        USING pg_temp.home_photo_candidate_page page
        WHERE candidate.photo_id = page.photo_id
          AND NOT EXISTS (
              SELECT 1
              FROM observations observation
              JOIN photos photo ON photo.id = page.photo_id
              WHERE observation.user_id = candidate.user_id
                AND observation.record_kind = 'photo'
                AND observation.record_id = page.photo_id::text
                AND photo.visibility = 'public'
                AND photo.tombstoned_at IS NULL
          );

        INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
        SELECT DISTINCT candidate.photo_id, candidate.uploaded_at
        FROM user_home_photo_candidates candidate
        JOIN pg_temp.home_photo_candidate_page page ON page.photo_id = candidate.photo_id
        JOIN sharing_settings setting ON setting.user_id = candidate.user_id AND setting.enabled
        ON CONFLICT (photo_id) DO UPDATE
        SET uploaded_at = EXCLUDED.uploaded_at;

        DELETE FROM user_home_shared_photo_candidates candidate
        USING pg_temp.home_photo_candidate_page page
        WHERE candidate.photo_id = page.photo_id
          AND NOT EXISTS (
              SELECT 1
              FROM photos photo
              WHERE photo.id = page.photo_id
                AND photo.visibility = 'public'
                AND photo.tombstoned_at IS NULL
                AND EXISTS (
                    SELECT 1
                    FROM observations observation
                    JOIN sharing_settings setting
                      ON setting.user_id = observation.user_id AND setting.enabled
                    WHERE observation.record_kind = 'photo'
                      AND observation.record_id = photo.id::text
                )
          );

        UPDATE user_home_candidates_backfill_progress
        SET cursor_photo_id = next_photo_id
        WHERE id = TRUE;
        COMMIT;
    END LOOP;

    DELETE FROM user_home_candidates_backfill_progress WHERE id = TRUE;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS observations_home_candidates ON observations;
CREATE TRIGGER observations_home_candidates
AFTER INSERT OR DELETE OR UPDATE OF user_id, record_kind, record_id ON observations
FOR EACH ROW EXECUTE FUNCTION sync_home_observation_candidate();
DROP TRIGGER IF EXISTS photos_home_candidates ON photos;
CREATE TRIGGER photos_home_candidates
AFTER INSERT OR UPDATE OF id, owner_nsid, visibility, uploaded_at, last_update, tombstoned_at, sizes_status, sizes_checked_at OR DELETE ON photos
FOR EACH ROW EXECUTE FUNCTION sync_home_photo_candidates();
DROP TRIGGER IF EXISTS enrichments_home_revision ON photo_enrichments;
CREATE TRIGGER enrichments_home_revision
AFTER INSERT OR UPDATE OR DELETE ON photo_enrichments
FOR EACH ROW EXECUTE FUNCTION sync_home_enrichment_revision();
DROP TRIGGER IF EXISTS sharing_settings_home_revision ON sharing_settings;
CREATE TRIGGER sharing_settings_home_revision
AFTER INSERT OR UPDATE OF enabled ON sharing_settings
FOR EACH ROW EXECUTE FUNCTION bump_home_sharing_revision();

-- Each page commits its candidate changes and cursor independently. The
-- trigger set is active first, so concurrent changes remain represented.
CALL backfill_home_photo_candidates(500);
DROP PROCEDURE backfill_home_photo_candidates(INTEGER);
DROP TABLE user_home_candidates_backfill_progress;
ANALYZE user_home_photo_candidates;
ANALYZE user_home_shared_photo_candidates;

-- +goose Down
DROP TRIGGER IF EXISTS sharing_settings_home_revision ON sharing_settings;
DROP TRIGGER IF EXISTS enrichments_home_revision ON photo_enrichments;
DROP TRIGGER IF EXISTS photos_home_candidates ON photos;
DROP TRIGGER IF EXISTS observations_home_candidates ON observations;
DROP FUNCTION IF EXISTS bump_home_sharing_revision();
DROP FUNCTION IF EXISTS bump_home_photo_revisions(BIGINT);
DROP FUNCTION IF EXISTS bump_home_user_revision(BIGINT);
DROP FUNCTION IF EXISTS sync_home_enrichment_revision();
DROP FUNCTION IF EXISTS sync_home_photo_candidates();
DROP FUNCTION IF EXISTS sync_home_observation_candidate();
DROP FUNCTION IF EXISTS sync_home_shared_photo_candidate(BIGINT);
DROP PROCEDURE IF EXISTS backfill_home_photo_candidates(INTEGER);
DROP TABLE IF EXISTS user_home_candidates_backfill_progress;
DROP INDEX CONCURRENTLY IF EXISTS work_items_home_contact_state_idx;
DROP INDEX CONCURRENTLY IF EXISTS work_items_user_activity_idx;
DROP INDEX CONCURRENTLY IF EXISTS downloads_user_activity_idx;
DROP INDEX CONCURRENTLY IF EXISTS sharing_settings_enabled_home_idx;
DROP INDEX CONCURRENTLY IF EXISTS user_home_shared_photo_candidates_recent_idx;
DROP INDEX CONCURRENTLY IF EXISTS user_home_photo_candidates_recent_idx;
DROP INDEX CONCURRENTLY IF EXISTS user_home_photo_candidates_photo_idx;
ALTER TABLE user_home_stats_refreshes DROP COLUMN IF EXISTS shared_revision, DROP COLUMN IF EXISTS source_revision;
ALTER TABLE user_home_stats DROP COLUMN IF EXISTS shared_revision, DROP COLUMN IF EXISTS source_revision;
DROP TABLE IF EXISTS user_home_shared_version;
DROP TABLE IF EXISTS user_home_stats_versions;
DROP TABLE IF EXISTS user_home_sharing_syncs;
DROP TABLE IF EXISTS user_home_shared_photo_candidates;
DROP TABLE IF EXISTS user_home_photo_candidates;
DELETE FROM app_settings WHERE key = 'home.stats_refresh_batch_size';
