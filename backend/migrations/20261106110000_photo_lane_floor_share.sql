-- +goose Up
INSERT INTO app_settings (key, value, user_overridable)
VALUES ('photo_lane_floor_share', '0.10', false);

-- +goose Down
DELETE FROM app_settings WHERE key = 'photo_lane_floor_share';
