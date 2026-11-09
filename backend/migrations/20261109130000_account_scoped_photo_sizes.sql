-- +goose Up
ALTER TABLE entity_facet_restrictions
    ADD COLUMN account_id BIGINT REFERENCES flickr_accounts (id) ON DELETE CASCADE;
ALTER TABLE entity_facet_restrictions
    DROP CONSTRAINT entity_facet_restrictions_pkey;
CREATE UNIQUE INDEX entity_facet_restrictions_key_scope_idx
    ON entity_facet_restrictions (entity_type, entity_id, facet, user_id, key_id)
    WHERE account_id IS NULL;
CREATE UNIQUE INDEX entity_facet_restrictions_account_scope_idx
    ON entity_facet_restrictions (entity_type, entity_id, facet, account_id)
    WHERE account_id IS NOT NULL;

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

-- Existing global confirmed results cannot be attributed to an anonymous or
-- OAuth request, so require a fresh anonymous or account-scoped result.
UPDATE photos
SET sizes='[]'::jsonb, sizes_status='provisional', sizes_checked_at=NULL
WHERE sizes_status='confirmed';

-- Legacy unavailable values do not identify which account failed. Re-evaluate them
-- against the requesting user's connected accounts and scoped restrictions.
UPDATE photos
SET sizes='[]'::jsonb, sizes_status='provisional', sizes_checked_at=NULL
WHERE sizes_status='unavailable';

-- +goose Down
DROP TABLE photo_account_sizes;
DROP INDEX entity_facet_restrictions_account_scope_idx;
DROP INDEX entity_facet_restrictions_key_scope_idx;
DELETE FROM entity_facet_restrictions WHERE account_id IS NOT NULL;
ALTER TABLE entity_facet_restrictions DROP COLUMN account_id;
ALTER TABLE entity_facet_restrictions
    ADD CONSTRAINT entity_facet_restrictions_pkey
    PRIMARY KEY (entity_type, entity_id, facet, user_id, key_id);
