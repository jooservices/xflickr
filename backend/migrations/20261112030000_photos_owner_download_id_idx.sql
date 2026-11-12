-- +goose NO TRANSACTION
-- +goose Up
-- Contact downloads filter one owner, order by photo ID, and stop after 501 candidates.
-- Build each partition index concurrently, then attach the partitioned parent index.
CREATE INDEX photos_owner_download_id_idx ON ONLY photos
    (owner_nsid, id) WHERE tombstoned_at IS NULL;

CREATE INDEX CONCURRENTLY photos_p00_owner_download_id_idx ON photos_p00
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p00_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p01_owner_download_id_idx ON photos_p01
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p01_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p02_owner_download_id_idx ON photos_p02
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p02_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p03_owner_download_id_idx ON photos_p03
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p03_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p04_owner_download_id_idx ON photos_p04
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p04_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p05_owner_download_id_idx ON photos_p05
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p05_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p06_owner_download_id_idx ON photos_p06
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p06_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p07_owner_download_id_idx ON photos_p07
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p07_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p08_owner_download_id_idx ON photos_p08
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p08_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p09_owner_download_id_idx ON photos_p09
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p09_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p10_owner_download_id_idx ON photos_p10
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p10_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p11_owner_download_id_idx ON photos_p11
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p11_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p12_owner_download_id_idx ON photos_p12
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p12_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p13_owner_download_id_idx ON photos_p13
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p13_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p14_owner_download_id_idx ON photos_p14
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p14_owner_download_id_idx;

CREATE INDEX CONCURRENTLY photos_p15_owner_download_id_idx ON photos_p15
    (owner_nsid, id) WHERE tombstoned_at IS NULL;
ALTER INDEX photos_owner_download_id_idx
    ATTACH PARTITION photos_p15_owner_download_id_idx;

-- +goose Down
DROP INDEX photos_owner_download_id_idx;
