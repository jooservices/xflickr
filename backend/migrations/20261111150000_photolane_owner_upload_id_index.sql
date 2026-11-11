-- +goose NO TRANSACTION
-- +goose Up
-- PostgreSQL cannot build a partitioned index concurrently. Create its parent
-- metadata first, then build each physical partition index concurrently.
CREATE INDEX photos_photo_lane_order_idx ON ONLY photos
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;

CREATE INDEX CONCURRENTLY photos_p00_photo_lane_order_idx ON photos_p00
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p00_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p01_photo_lane_order_idx ON photos_p01
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p01_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p02_photo_lane_order_idx ON photos_p02
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p02_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p03_photo_lane_order_idx ON photos_p03
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p03_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p04_photo_lane_order_idx ON photos_p04
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p04_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p05_photo_lane_order_idx ON photos_p05
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p05_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p06_photo_lane_order_idx ON photos_p06
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p06_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p07_photo_lane_order_idx ON photos_p07
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p07_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p08_photo_lane_order_idx ON photos_p08
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p08_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p09_photo_lane_order_idx ON photos_p09
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p09_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p10_photo_lane_order_idx ON photos_p10
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p10_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p11_photo_lane_order_idx ON photos_p11
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p11_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p12_photo_lane_order_idx ON photos_p12
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p12_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p13_photo_lane_order_idx ON photos_p13
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p13_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p14_photo_lane_order_idx ON photos_p14
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p14_photo_lane_order_idx;
CREATE INDEX CONCURRENTLY photos_p15_photo_lane_order_idx ON photos_p15
    (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE tombstoned_at IS NULL;
ALTER INDEX photos_photo_lane_order_idx
    ATTACH PARTITION photos_p15_photo_lane_order_idx;

-- +goose Down
DROP INDEX photos_photo_lane_order_idx;
