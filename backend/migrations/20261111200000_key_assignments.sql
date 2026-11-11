-- +goose Up
CREATE TABLE key_assignments (
    key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    node TEXT NOT NULL REFERENCES nodes(name) ON DELETE RESTRICT,
    since TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL CHECK (reason = 'initial')
);

-- +goose Down
DROP TABLE key_assignments;
