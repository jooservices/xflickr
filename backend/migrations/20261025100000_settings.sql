-- +goose Up
CREATE TABLE app_settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    user_overridable BOOLEAN NOT NULL DEFAULT TRUE,
    min DOUBLE PRECISION,
    max DOUBLE PRECISION,
    CONSTRAINT app_settings_caps_ordered CHECK (min IS NULL OR max IS NULL OR min <= max)
);

CREATE TABLE user_settings (
    user_id BIGINT NOT NULL,
    key TEXT NOT NULL REFERENCES app_settings(key) ON DELETE CASCADE,
    value JSONB NOT NULL,
    PRIMARY KEY (user_id, key)
);

-- Duration values are integer seconds (24h = 86400, 45m = 2700, 30s = 30).
-- max_depth defaults to 2 and its admin cap is 4. daily_call_limit 0 means unlimited.
INSERT INTO app_settings (key, value, user_overridable, min, max) VALUES
    ('max_depth', '2'::jsonb, TRUE, 1, 4),
    ('change_check_interval', '86400'::jsonb, TRUE, NULL, NULL),
    ('daily_call_limit', '0'::jsonb, TRUE, NULL, NULL),
    ('quota_reserve', '100'::jsonb, TRUE, NULL, NULL),
    ('per_second_rate', '4'::jsonb, TRUE, NULL, NULL),
    ('burst', '5'::jsonb, TRUE, NULL, NULL),
    ('person_lane_share', '0.30'::jsonb, TRUE, NULL, NULL),
    ('zip_ttl', '86400'::jsonb, TRUE, NULL, NULL),
    ('retry_max', '5'::jsonb, TRUE, NULL, NULL),
    ('retry_backoff', '30'::jsonb, TRUE, NULL, NULL),
    ('node_cooldown', '2700'::jsonb, TRUE, NULL, NULL),
    ('group_pools_enabled', 'false'::jsonb, TRUE, NULL, NULL);

-- +goose Down
DROP TABLE user_settings;
DROP TABLE app_settings;
