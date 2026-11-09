-- +goose Up
INSERT INTO app_settings (key, value, user_overridable, min, max) VALUES
    ('ops.health_interval', '60'::jsonb, FALSE, 10, 3600),
    ('ops.health_statement_timeout', '5'::jsonb, FALSE, 1, 60)
ON CONFLICT (key) DO NOTHING;

-- Cheap lookup for photo.sizes river jobs by kind while active. river_job is
-- small (~tens of thousands of rows), so a plain expression index is enough.
CREATE INDEX river_job_args_kind_active_idx
    ON river_job ((args->>'kind'))
    WHERE state IN ('available', 'pending', 'scheduled', 'retryable', 'running');

-- +goose Down
DROP INDEX IF EXISTS river_job_args_kind_active_idx;
DELETE FROM app_settings WHERE key IN ('ops.health_interval', 'ops.health_statement_timeout');
