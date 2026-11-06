-- +goose Up
-- A common registry also retains markers for IDs not yet in the catalog.
CREATE TABLE entity_states (
    entity_type TEXT NOT NULL CHECK (entity_type IN ('person','album','gallery','group')),
    entity_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'gone' CHECK (state = 'gone'),
    first_seen_at TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL,
    PRIMARY KEY (entity_type, entity_id)
);
CREATE TABLE entity_facet_restrictions (
    entity_type TEXT NOT NULL CHECK (entity_type IN ('person','photo','album','gallery','group')),
    entity_id TEXT NOT NULL,
    facet TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    key_id BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'restricted' CHECK (state = 'restricted'),
    first_seen_at TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL,
    recheck_at TIMESTAMPTZ NOT NULL,
    photo_updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (entity_type, entity_id, facet, user_id, key_id)
);
INSERT INTO app_settings (key, value, user_overridable)
VALUES ('restricted_recheck_interval', '2592000', false);

-- Visibility changes reopen restrictions even when Flickr's version is unchanged.
-- +goose StatementBegin
CREATE FUNCTION reopen_restricted_photo_facets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.visibility IS DISTINCT FROM OLD.visibility THEN
        DELETE FROM entity_facet_restrictions WHERE entity_type='photo' AND entity_id=NEW.id::text;
        IF NEW.sizes_status='unavailable' THEN
            UPDATE photos SET sizes_status='none',sizes_checked_at=NULL WHERE id=NEW.id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER reopen_restricted_photo_facets AFTER UPDATE OF visibility ON photos
FOR EACH ROW EXECUTE FUNCTION reopen_restricted_photo_facets();

-- +goose Down
DROP TRIGGER reopen_restricted_photo_facets ON photos;
DROP FUNCTION reopen_restricted_photo_facets();
DELETE FROM app_settings WHERE key='restricted_recheck_interval';
DROP TABLE entity_facet_restrictions;
DROP TABLE entity_states;
