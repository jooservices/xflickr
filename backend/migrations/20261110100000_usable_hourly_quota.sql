-- +goose Up
INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('usable_hourly_quota', '3400'::jsonb, false, 1, NULL);

-- +goose Down
DELETE FROM app_settings WHERE key = 'usable_hourly_quota';
