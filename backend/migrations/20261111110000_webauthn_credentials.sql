-- +goose Up
CREATE TABLE webauthn_credentials (
    credential_id BYTEA PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_json JSONB NOT NULL
);

-- +goose Down
DROP TABLE webauthn_credentials;
