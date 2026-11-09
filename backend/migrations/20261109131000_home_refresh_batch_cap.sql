-- +goose Up
UPDATE app_settings
SET value = to_jsonb(LEAST(GREATEST((value #>> '{}')::bigint, 250), 1000)),
    min = 250,
    max = 1000
WHERE key = 'home.stats_refresh_batch_size';

-- +goose Down
UPDATE app_settings
SET value = to_jsonb(LEAST(GREATEST((value #>> '{}')::bigint, 250), 1000000)),
    min = 250,
    max = 1000000
WHERE key = 'home.stats_refresh_batch_size';
