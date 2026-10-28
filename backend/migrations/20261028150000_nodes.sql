-- +goose Up
CREATE TABLE nodes (
    name TEXT PRIMARY KEY,
    roles TEXT[] NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL DEFAULT '',
    egress_ip INET,
    state TEXT NOT NULL DEFAULT 'offline',
    cooling_until TIMESTAMPTZ,
    last_heartbeat_at TIMESTAMPTZ,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    token_hash BYTEA NOT NULL UNIQUE,
    CONSTRAINT nodes_name_len CHECK (char_length(name) BETWEEN 1 AND 128),
    CONSTRAINT nodes_roles_count CHECK (cardinality(roles) BETWEEN 1 AND 32),
    CONSTRAINT nodes_state_known CHECK (state IN ('online', 'cooling', 'offline', 'disabled')),
    CONSTRAINT nodes_metrics_object CHECK (jsonb_typeof(metrics) = 'object'),
    CONSTRAINT nodes_token_hash_len CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX nodes_last_heartbeat_at_idx ON nodes (last_heartbeat_at)
    WHERE state <> 'disabled';

-- +goose Down
DROP TABLE nodes;
