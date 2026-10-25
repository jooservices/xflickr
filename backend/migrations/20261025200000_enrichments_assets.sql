-- +goose Up
CREATE TABLE photo_enrichments (
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('caption', 'embedding_meta')),
    model TEXT NOT NULL,
    model_version TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, kind, model)
);

CREATE TABLE assets (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    size_label TEXT NOT NULL,
    sha256 BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    bytes BIGINT NOT NULL CHECK (bytes >= 0),
    blob_key TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX assets_expired_idx ON assets (expires_at, id);

-- +goose Down
DROP TABLE assets;
DROP TABLE photo_enrichments;
