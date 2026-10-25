-- +goose Up
CREATE INDEX photos_sizes_pending_idx ON photos (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE sizes_status <> 'confirmed' AND tombstoned_at IS NULL;

-- Streams of the photo lane's person ranking: a live selection, then a star,
-- then depth. Each is read in order with a LIMIT, so the ranking never scans
-- every interest.
CREATE INDEX crawl_interests_selected_idx ON crawl_interests (starred DESC, depth, person_nsid)
    WHERE selected_until IS NOT NULL;
CREATE INDEX crawl_interests_starred_idx ON crawl_interests (depth, person_nsid)
    WHERE starred;
CREATE INDEX crawl_interests_depth_idx ON crawl_interests (depth, person_nsid);

-- +goose Down
DROP INDEX crawl_interests_depth_idx;
DROP INDEX crawl_interests_starred_idx;
DROP INDEX crawl_interests_selected_idx;
DROP INDEX photos_sizes_pending_idx;
