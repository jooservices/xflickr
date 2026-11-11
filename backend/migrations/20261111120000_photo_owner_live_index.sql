-- +goose NO TRANSACTION
-- +goose Up
-- Optional facets also select already-confirmed photos. Keep those reads
-- ordered and bounded per owner without scanning every photo for that person.
CREATE INDEX photos_owner_live_idx ON ONLY photos
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
CREATE INDEX CONCURRENTLY photos_p00_owner_live_idx ON photos_p00
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p00_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p01_owner_live_idx ON photos_p01
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p01_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p02_owner_live_idx ON photos_p02
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p02_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p03_owner_live_idx ON photos_p03
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p03_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p04_owner_live_idx ON photos_p04
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p04_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p05_owner_live_idx ON photos_p05
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p05_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p06_owner_live_idx ON photos_p06
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p06_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p07_owner_live_idx ON photos_p07
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p07_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p08_owner_live_idx ON photos_p08
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p08_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p09_owner_live_idx ON photos_p09
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p09_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p10_owner_live_idx ON photos_p10
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p10_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p11_owner_live_idx ON photos_p11
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p11_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p12_owner_live_idx ON photos_p12
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p12_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p13_owner_live_idx ON photos_p13
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p13_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p14_owner_live_idx ON photos_p14
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p14_owner_live_idx;
CREATE INDEX CONCURRENTLY photos_p15_owner_live_idx ON photos_p15
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_live_idx ATTACH PARTITION photos_p15_owner_live_idx;

-- +goose Down
DROP INDEX photos_owner_live_idx;
