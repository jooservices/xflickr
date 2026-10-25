-- +goose Up
ALTER TABLE photos DROP CONSTRAINT photos_sizes_status_check;
ALTER TABLE photos ADD CONSTRAINT photos_sizes_status_check
    CHECK (sizes_status IN ('none', 'provisional', 'confirmed', 'unavailable'));

DROP INDEX photos_sizes_pending_idx;
CREATE INDEX photos_sizes_pending_idx ON photos (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE (sizes_status NOT IN ('confirmed', 'unavailable')
           OR (sizes_status = 'confirmed' AND sizes_checked_at < last_update))
      AND tombstoned_at IS NULL;

-- +goose Down
DROP INDEX photos_sizes_pending_idx;
CREATE INDEX photos_sizes_pending_idx ON photos (owner_nsid, uploaded_at DESC NULLS LAST, id DESC)
    WHERE sizes_status <> 'confirmed' AND tombstoned_at IS NULL;

UPDATE photos SET sizes_status = 'none' WHERE sizes_status = 'unavailable';
ALTER TABLE photos DROP CONSTRAINT photos_sizes_status_check;
ALTER TABLE photos ADD CONSTRAINT photos_sizes_status_check
    CHECK (sizes_status IN ('none', 'provisional', 'confirmed'));
