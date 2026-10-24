-- +goose Up
CREATE TABLE auth_settings (
    key TEXT PRIMARY KEY,
    value BOOLEAN NOT NULL
);

INSERT INTO auth_settings (key, value) VALUES ('registration_open', FALSE);
DELETE FROM app_settings WHERE key = 'registration_open';

-- +goose Down
DROP TABLE auth_settings;
