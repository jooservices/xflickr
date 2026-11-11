-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE entity_facet_restrictions ADD COLUMN account_id BIGINT;
ALTER TABLE entity_facet_restrictions
    ADD CONSTRAINT entity_facet_restrictions_account_id_fkey
    FOREIGN KEY (account_id) REFERENCES flickr_accounts (id) ON DELETE CASCADE NOT VALID;
ALTER TABLE entity_facet_restrictions
    VALIDATE CONSTRAINT entity_facet_restrictions_account_id_fkey;

CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_key_scope_idx
    ON entity_facet_restrictions (entity_type, entity_id, facet, user_id, key_id)
    WHERE account_id IS NULL;
CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_account_scope_idx
    ON entity_facet_restrictions (entity_type, entity_id, facet, account_id)
    WHERE account_id IS NOT NULL;

-- Replacing the old key-only primary key is now a short catalog-only lock;
-- both replacement indexes already hold the corresponding uniqueness rules.
ALTER TABLE entity_facet_restrictions
    DROP CONSTRAINT entity_facet_restrictions_pkey;

CREATE TABLE photo_account_sizes (
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES flickr_accounts (id) ON DELETE CASCADE,
    photo_updated_at TIMESTAMPTZ NOT NULL,
    photo_visibility TEXT NOT NULL CHECK (photo_visibility IN ('public', 'friends', 'family', 'private')),
    sizes JSONB NOT NULL DEFAULT '[]'::jsonb,
    sizes_status TEXT NOT NULL CHECK (sizes_status IN ('confirmed', 'unavailable')),
    sizes_checked_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (photo_id, account_id)
);
CREATE INDEX photo_account_sizes_account_photo_idx
    ON photo_account_sizes (account_id, photo_id);

-- Keep existing global photo outcomes intact. New OAuth outcomes and failures
-- are written to photo_account_sizes and never replace globally reusable data.

-- +goose Down
DROP TABLE photo_account_sizes;
DELETE FROM entity_facet_restrictions WHERE account_id IS NOT NULL;
CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_legacy_pkey_idx
    ON entity_facet_restrictions (entity_type, entity_id, facet, user_id, key_id);
ALTER TABLE entity_facet_restrictions
    ADD CONSTRAINT entity_facet_restrictions_pkey
    PRIMARY KEY USING INDEX entity_facet_restrictions_legacy_pkey_idx;
DROP INDEX CONCURRENTLY entity_facet_restrictions_account_scope_idx;
DROP INDEX CONCURRENTLY entity_facet_restrictions_key_scope_idx;
ALTER TABLE entity_facet_restrictions DROP COLUMN account_id;
