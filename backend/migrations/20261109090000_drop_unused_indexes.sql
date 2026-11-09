-- Drop photos indexes that the live audit (2026-11-02..2026-11-09) showed as
-- unused or redundant, and that no current or in-flight query needs.
--
-- photos_last_update_idx: zero scans; ListPhotos filters by owner_nsid first, so
-- a global last_update index is not selected. JOOXF-345 / JOOXF-348 do not query
-- photos by last_update.
-- photos_owner_nsid_idx: leading prefix of photos_owner_last_seen_idx and of
-- photos_sizes_pending_idx; three scans in the audit window.
--
-- Drop through the partitioned parent so every partition child goes with it.
-- Plain DROP INDEX (not CONCURRENTLY): goose runs inside a transaction and DROP
-- of a partitioned index is short-lived per partition.

-- +goose Up
DROP INDEX photos_last_update_idx;
DROP INDEX photos_owner_nsid_idx;

-- +goose Down
CREATE INDEX photos_last_update_idx ON photos (last_update);
CREATE INDEX photos_owner_nsid_idx ON photos (owner_nsid);
