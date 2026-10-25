-- +goose Up
-- Store compressed Flickr payloads in a JSONB envelope. The migration keeps
-- existing zstd bytes by base64-encoding them into the envelope.
ALTER TABLE photos ALTER COLUMN raw TYPE JSONB
USING jsonb_build_object('encoding', 'zstd', 'data', encode(raw, 'base64'));

-- +goose Down
ALTER TABLE photos ALTER COLUMN raw TYPE BYTEA
USING decode(raw->>'data', 'base64');
