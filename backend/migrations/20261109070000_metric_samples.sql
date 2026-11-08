-- +goose Up
CREATE TABLE metric_samples (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels_hash TEXT NOT NULL,
    node TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    bucket TIMESTAMPTZ NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    CONSTRAINT metric_samples_name_len CHECK (char_length(name) BETWEEN 1 AND 128),
    CONSTRAINT metric_samples_labels_object CHECK (jsonb_typeof(labels) = 'object'),
    CONSTRAINT metric_samples_labels_hash_len CHECK (char_length(labels_hash) = 64),
    CONSTRAINT metric_samples_unique UNIQUE (name, labels_hash, bucket)
);

CREATE INDEX metric_samples_bucket_name_idx ON metric_samples (bucket, name);
CREATE INDEX metric_samples_name_bucket_idx ON metric_samples (name, bucket DESC);

CREATE TABLE metric_rollups (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels_hash TEXT NOT NULL,
    node TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    bucket TIMESTAMPTZ NOT NULL,
    resolution TEXT NOT NULL DEFAULT '1h',
    avg DOUBLE PRECISION NOT NULL,
    min DOUBLE PRECISION NOT NULL,
    max DOUBLE PRECISION NOT NULL,
    p95 DOUBLE PRECISION,
    samples INTEGER NOT NULL,
    CONSTRAINT metric_rollups_name_len CHECK (char_length(name) BETWEEN 1 AND 128),
    CONSTRAINT metric_rollups_labels_object CHECK (jsonb_typeof(labels) = 'object'),
    CONSTRAINT metric_rollups_labels_hash_len CHECK (char_length(labels_hash) = 64),
    CONSTRAINT metric_rollups_resolution_known CHECK (resolution IN ('1h')),
    CONSTRAINT metric_rollups_unique UNIQUE (name, labels_hash, bucket, resolution)
);

CREATE INDEX metric_rollups_bucket_name_idx ON metric_rollups (bucket, name);

INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('metrics.retention_days', '30'::jsonb, FALSE, 1, 365);

-- +goose Down
DELETE FROM app_settings WHERE key = 'metrics.retention_days';
DROP TABLE metric_rollups;
DROP TABLE metric_samples;
