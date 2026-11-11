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

-- These child indexes add legacy-unavailable and placeholder rows to the
-- existing pending index without blocking writes across the partition scans.
CREATE INDEX photos_sizes_restricted_idx ON ONLY photos
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p00_sizes_restricted_idx ON photos_p00
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p00_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p01_sizes_restricted_idx ON photos_p01
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p01_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p02_sizes_restricted_idx ON photos_p02
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p02_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p03_sizes_restricted_idx ON photos_p03
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p03_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p04_sizes_restricted_idx ON photos_p04
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p04_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p05_sizes_restricted_idx ON photos_p05
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p05_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p06_sizes_restricted_idx ON photos_p06
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p06_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p07_sizes_restricted_idx ON photos_p07
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p07_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p08_sizes_restricted_idx ON photos_p08
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p08_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p09_sizes_restricted_idx ON photos_p09
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p09_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p10_sizes_restricted_idx ON photos_p10
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p10_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p11_sizes_restricted_idx ON photos_p11
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p11_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p12_sizes_restricted_idx ON photos_p12
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p12_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p13_sizes_restricted_idx ON photos_p13
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p13_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p14_sizes_restricted_idx ON photos_p14
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p14_sizes_restricted_idx;
CREATE INDEX CONCURRENTLY photos_p15_sizes_restricted_idx ON photos_p15
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status = 'unavailable' OR server = '65535')
      AND tombstoned_at IS NULL;
ALTER INDEX photos_sizes_restricted_idx ATTACH PARTITION photos_p15_sizes_restricted_idx;

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
DROP INDEX photos_sizes_restricted_idx;
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
