-- +goose NO TRANSACTION
-- +goose Up
-- The photo lane checks "is a job with this dedupe key already waiting?" before every enqueue
-- (schedule/adapters/river/photolane.go). Without this partial expression index that check scans
-- river_job. CREATE INDEX CONCURRENTLY leaves an invalid index behind if it is interrupted, so a
-- retry first drops any leftover (concurrently, never blocking writes).
DROP INDEX CONCURRENTLY IF EXISTS river_job_photo_dedupe_key_idx;
CREATE INDEX CONCURRENTLY river_job_photo_dedupe_key_idx
    ON river_job ((args->'args'->>'dedupe_key'))
    WHERE state IN ('available', 'pending', 'scheduled', 'retryable', 'running');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS river_job_photo_dedupe_key_idx;
