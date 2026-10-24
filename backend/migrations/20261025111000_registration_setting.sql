-- +goose Up
INSERT INTO app_settings (key, value, user_overridable)
VALUES ('registration_open', 'false'::jsonb, FALSE);

-- +goose Down
DELETE FROM app_settings WHERE key = 'registration_open';
