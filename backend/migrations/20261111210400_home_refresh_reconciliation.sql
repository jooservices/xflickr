-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE user_home_stats
    ADD COLUMN IF NOT EXISTS estimated BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE user_home_stats_refreshes
    ADD COLUMN IF NOT EXISTS restart_required BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS user_home_candidates_backfill_progress (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    cursor_photo_id BIGINT NOT NULL DEFAULT 0
);

-- Keep the bounded backfill available so an interrupted or raced page can be
-- reconciled without weakening request-time permission checks.
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

    INSERT INTO user_home_candidates_backfill_progress (id, cursor_photo_id)
    VALUES (TRUE, 0) ON CONFLICT (id) DO NOTHING;

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

-- Invalidated rows are permission-sensitive. Public catalog additions can
-- publish as estimates, while removals restart active refreshes from page one.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION invalidate_home_photo_access() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_photo BIGINT;
    revoked BOOLEAN;
BEGIN
    IF TG_OP = 'DELETE' THEN
        affected_photo := OLD.id;
        revoked := OLD.visibility = 'public' AND OLD.tombstoned_at IS NULL;
    ELSE
        affected_photo := OLD.id;
        revoked := OLD.visibility = 'public'
          AND OLD.tombstoned_at IS NULL
          AND (NEW.visibility <> 'public' OR NEW.tombstoned_at IS NOT NULL);
    END IF;
    IF NOT revoked THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;

    UPDATE user_home_stats_refreshes refresh
    SET restart_required = TRUE
    WHERE refresh.user_id IN (
        SELECT candidate.user_id
        FROM user_home_photo_candidates candidate
        WHERE candidate.photo_id = affected_photo
    );
    UPDATE user_home_stats stats
    SET invalidated_at = statement_timestamp()
    WHERE stats.user_id IN (
        SELECT candidate.user_id
        FROM user_home_photo_candidates candidate
        WHERE candidate.photo_id = affected_photo
    );

    IF EXISTS (
        SELECT 1 FROM user_home_shared_photo_candidates candidate
        WHERE candidate.photo_id = affected_photo
    ) THEN
        UPDATE user_home_stats_refreshes SET restart_required = TRUE;
        UPDATE user_home_stats SET invalidated_at = statement_timestamp();
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION invalidate_home_observation_access() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_photo BIGINT;
    removed BOOLEAN;
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.record_kind IS NOT DISTINCT FROM NEW.record_kind
       AND OLD.record_id IS NOT DISTINCT FROM NEW.record_id THEN
        RETURN NEW;
    END IF;
    IF OLD.record_kind <> 'photo' OR OLD.record_id !~ '^[0-9]+$' THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;

    affected_photo := OLD.record_id::bigint;
    UPDATE user_home_stats_refreshes SET restart_required = TRUE
    WHERE user_id = OLD.user_id;
    UPDATE user_home_stats SET invalidated_at = statement_timestamp()
    WHERE user_id = OLD.user_id;

    SELECT EXISTS (
        SELECT 1 FROM sharing_settings setting
        WHERE setting.user_id = OLD.user_id AND setting.enabled
    ) AND NOT EXISTS (
        SELECT 1
        FROM observations observation
        JOIN sharing_settings setting
          ON setting.user_id = observation.user_id AND setting.enabled
        WHERE observation.record_kind = 'photo'
          AND observation.record_id = affected_photo::text
    ) INTO removed;
    IF removed THEN
        UPDATE user_home_stats_refreshes SET restart_required = TRUE;
        UPDATE user_home_stats SET invalidated_at = statement_timestamp();
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION invalidate_home_sharing_access() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    changed BOOLEAN;
BEGIN
    IF TG_OP = 'INSERT' THEN
        changed := TRUE;
    ELSE
        changed := OLD.enabled IS DISTINCT FROM NEW.enabled;
    END IF;
    IF changed THEN
        UPDATE user_home_stats_refreshes SET restart_required = TRUE;
        UPDATE user_home_stats SET invalidated_at = statement_timestamp();
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS home_stats_photo_access ON photos;
CREATE TRIGGER home_stats_photo_access
AFTER UPDATE OF visibility, tombstoned_at OR DELETE ON photos
FOR EACH ROW EXECUTE FUNCTION invalidate_home_photo_access();
DROP TRIGGER IF EXISTS home_stats_observation_access ON observations;
CREATE TRIGGER home_stats_observation_access
AFTER DELETE OR UPDATE OF user_id, record_kind, record_id ON observations
FOR EACH ROW EXECUTE FUNCTION invalidate_home_observation_access();
DROP TRIGGER IF EXISTS home_stats_sharing_access ON sharing_settings;
CREATE TRIGGER home_stats_sharing_access
AFTER INSERT OR UPDATE OF enabled ON sharing_settings
FOR EACH ROW EXECUTE FUNCTION invalidate_home_sharing_access();

-- +goose Down
DROP TRIGGER IF EXISTS home_stats_sharing_access ON sharing_settings;
DROP TRIGGER IF EXISTS home_stats_observation_access ON observations;
DROP TRIGGER IF EXISTS home_stats_photo_access ON photos;
DROP FUNCTION IF EXISTS invalidate_home_sharing_access();
DROP FUNCTION IF EXISTS invalidate_home_observation_access();
DROP FUNCTION IF EXISTS invalidate_home_photo_access();
DROP PROCEDURE IF EXISTS backfill_home_photo_candidates(INTEGER);
DROP TABLE IF EXISTS user_home_candidates_backfill_progress;
ALTER TABLE user_home_stats_refreshes DROP COLUMN IF EXISTS restart_required;
ALTER TABLE user_home_stats DROP COLUMN IF EXISTS estimated;
