-- +goose Up
-- +goose StatementBegin
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
        IF NEW.enabled THEN
            INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
            SELECT candidate.photo_id, candidate.uploaded_at
            FROM user_home_photo_candidates candidate
            JOIN photos photo ON photo.id = candidate.photo_id
            WHERE candidate.user_id = NEW.user_id
              AND photo.visibility = 'public'
              AND photo.tombstoned_at IS NULL
            ON CONFLICT (photo_id) DO UPDATE
            SET uploaded_at = EXCLUDED.uploaded_at;
        ELSE
            DELETE FROM user_home_shared_photo_candidates candidate
            USING user_home_photo_candidates affected
            WHERE affected.user_id = OLD.user_id
              AND affected.photo_id = candidate.photo_id
              AND NOT EXISTS (
                  SELECT 1
                  FROM observations observation
                  JOIN sharing_settings setting
                    ON setting.user_id = observation.user_id AND setting.enabled
                  JOIN photos photo ON photo.id::text = observation.record_id
                  WHERE observation.record_kind = 'photo'
                    AND observation.record_id = candidate.photo_id::text
                    AND photo.visibility = 'public'
                    AND photo.tombstoned_at IS NULL
              );
        END IF;
        UPDATE user_home_shared_version SET revision = revision + 1 WHERE id = TRUE;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Catch up rows left by earlier asynchronous permission changes before the
-- trigger makes subsequent setting changes synchronous.
INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
SELECT candidate.photo_id, candidate.uploaded_at
FROM user_home_photo_candidates candidate
JOIN sharing_settings setting ON setting.user_id = candidate.user_id AND setting.enabled
JOIN photos photo ON photo.id = candidate.photo_id
WHERE photo.visibility = 'public'
  AND photo.tombstoned_at IS NULL
ON CONFLICT (photo_id) DO UPDATE SET uploaded_at = EXCLUDED.uploaded_at;

DELETE FROM user_home_shared_photo_candidates candidate
WHERE NOT EXISTS (
    SELECT 1
    FROM observations observation
    JOIN sharing_settings setting
      ON setting.user_id = observation.user_id AND setting.enabled
    JOIN photos photo ON photo.id::text = observation.record_id
    WHERE observation.record_kind = 'photo'
      AND observation.record_id = candidate.photo_id::text
      AND photo.visibility = 'public'
      AND photo.tombstoned_at IS NULL
);

-- +goose Down
-- +goose StatementBegin
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
