-- +goose Up
CREATE TABLE download_parts (
    id BIGSERIAL PRIMARY KEY,
    download_id BIGINT NOT NULL REFERENCES downloads(id) ON DELETE CASCADE,
    part_number INTEGER NOT NULL CHECK (part_number > 0),
    blob_key TEXT NOT NULL,
    bytes BIGINT NOT NULL CHECK (bytes >= 0),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (download_id, part_number)
);
CREATE INDEX download_parts_expired_idx ON download_parts(expires_at, id);

-- +goose Down
DROP TABLE download_parts;
