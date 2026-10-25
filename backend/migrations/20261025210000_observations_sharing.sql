-- +goose Up
CREATE TABLE observations (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    flickr_account_id BIGINT NOT NULL REFERENCES flickr_accounts (id) ON DELETE CASCADE,
    record_kind TEXT NOT NULL CHECK (record_kind IN ('person', 'photo')),
    record_id TEXT NOT NULL CHECK (char_length(record_id) > 0),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, flickr_account_id, record_kind, record_id)
) PARTITION BY HASH (record_id);

CREATE TABLE observations_p00 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 0);
CREATE TABLE observations_p01 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 1);
CREATE TABLE observations_p02 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 2);
CREATE TABLE observations_p03 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 3);
CREATE TABLE observations_p04 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 4);
CREATE TABLE observations_p05 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 5);
CREATE TABLE observations_p06 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 6);
CREATE TABLE observations_p07 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 7);
CREATE TABLE observations_p08 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 8);
CREATE TABLE observations_p09 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 9);
CREATE TABLE observations_p10 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 10);
CREATE TABLE observations_p11 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 11);
CREATE TABLE observations_p12 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 12);
CREATE TABLE observations_p13 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 13);
CREATE TABLE observations_p14 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 14);
CREATE TABLE observations_p15 PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER 15);

CREATE TABLE sharing_settings (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO sharing_settings (user_id) SELECT id FROM users ON CONFLICT DO NOTHING;

-- +goose StatementBegin
CREATE FUNCTION create_default_sharing_setting() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO sharing_settings (user_id) VALUES (NEW.id) ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER users_default_sharing_setting
AFTER INSERT ON users FOR EACH ROW EXECUTE FUNCTION create_default_sharing_setting();

-- +goose Down
DROP TRIGGER users_default_sharing_setting ON users;
DROP FUNCTION create_default_sharing_setting();
DROP TABLE sharing_settings;
DROP TABLE observations;
