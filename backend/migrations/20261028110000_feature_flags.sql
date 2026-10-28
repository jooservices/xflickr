-- +goose Up
CREATE TABLE feature_flags (
    name TEXT PRIMARY KEY
        CHECK (name ~ '^[a-z][a-z0-9_.-]{0,127}$'),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    audience TEXT NOT NULL DEFAULT 'all'
        CHECK (audience IN ('all', 'admins', 'user_ids')),
    user_ids BIGINT[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT feature_flags_audience_users CHECK (
        (audience = 'user_ids' AND cardinality(user_ids) BETWEEN 1 AND 10000)
        OR (audience <> 'user_ids' AND cardinality(user_ids) = 0)
    ),
    CONSTRAINT feature_flags_no_null_user_ids CHECK (array_position(user_ids, NULL) IS NULL)
);

-- +goose Down
DROP TABLE feature_flags;
