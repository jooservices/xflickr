-- +goose Up
INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ('contacts_refresh_interval', '86400'::jsonb, TRUE, 3600, 2592000);

-- +goose Down
DELETE FROM app_settings WHERE key = 'contacts_refresh_interval';
