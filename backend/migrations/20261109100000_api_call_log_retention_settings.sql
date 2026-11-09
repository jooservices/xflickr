-- +goose Up
INSERT INTO app_settings (key, value, user_overridable, min, max) VALUES
    ('maintenance.api_call_log_retention_days', '30'::jsonb, FALSE, 7, 365),
    ('maintenance.api_call_log_dry_run', 'false'::jsonb, FALSE, NULL, NULL)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM app_settings WHERE key IN (
    'maintenance.api_call_log_retention_days',
    'maintenance.api_call_log_dry_run'
);
