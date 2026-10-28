-- +goose Up
ALTER TABLE photos
    ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX photos_owner_last_seen_idx ON photos (owner_nsid, last_seen_at);

ALTER TABLE favorites
    ADD COLUMN removed_at TIMESTAMPTZ;
CREATE INDEX favorites_active_person_seen_idx
    ON favorites (person_nsid, seen_at)
    WHERE removed_at IS NULL;

ALTER TABLE crawl_interests
    ADD COLUMN last_reconciled_at TIMESTAMPTZ NOT NULL DEFAULT now();

INSERT INTO app_settings (key, value, user_overridable)
VALUES ('reconcile_interval', '2592000'::jsonb, TRUE);

-- +goose StatementBegin
CREATE FUNCTION record_photo_visibility_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO outbox_events (topic, key, payload)
    VALUES (
        'photo.visibility_changed',
        NEW.id::text,
        jsonb_build_object(
            'photo_id', NEW.id,
            'owner_nsid', NEW.owner_nsid,
            'from', OLD.visibility,
            'to', NEW.visibility
        )
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER photos_visibility_changed_outbox
    AFTER UPDATE OF visibility ON photos
    FOR EACH ROW
    WHEN (OLD.visibility IS DISTINCT FROM NEW.visibility)
    EXECUTE FUNCTION record_photo_visibility_change();

-- +goose Down
DROP TRIGGER photos_visibility_changed_outbox ON photos;
DROP FUNCTION record_photo_visibility_change();
DELETE FROM app_settings WHERE key = 'reconcile_interval';
ALTER TABLE crawl_interests DROP COLUMN last_reconciled_at;
DROP INDEX favorites_active_person_seen_idx;
ALTER TABLE favorites DROP COLUMN removed_at;
DROP INDEX photos_owner_last_seen_idx;
ALTER TABLE photos DROP COLUMN last_seen_at;
