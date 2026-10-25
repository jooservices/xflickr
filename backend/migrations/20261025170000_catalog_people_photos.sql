-- +goose Up
CREATE TABLE people (
    nsid TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    realname TEXT NOT NULL DEFAULT '',
    profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    icon TEXT NOT NULL DEFAULT '',
    photos_count BIGINT NOT NULL DEFAULT 0 CHECK (photos_count >= 0),
    last_seen_at TIMESTAMPTZ,
    facet_state JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE photos (
    id BIGINT NOT NULL,
    owner_nsid TEXT NOT NULL REFERENCES people (nsid),
    secret TEXT NOT NULL DEFAULT '',
    server TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    taken_at TIMESTAMPTZ,
    uploaded_at TIMESTAMPTZ,
    last_update TIMESTAMPTZ NOT NULL,
    visibility TEXT NOT NULL CHECK (visibility IN ('public', 'friends', 'family', 'private')),
    license TEXT NOT NULL DEFAULT '',
    media TEXT NOT NULL DEFAULT '',
    geo JSONB NOT NULL DEFAULT '{}'::jsonb,
    sizes JSONB NOT NULL DEFAULT '[]'::jsonb,
    sizes_status TEXT NOT NULL DEFAULT 'none' CHECK (sizes_status IN ('none', 'provisional', 'confirmed')),
    sizes_checked_at TIMESTAMPTZ,
    raw BYTEA NOT NULL,
    content_hash BYTEA NOT NULL CHECK (octet_length(content_hash) = 32),
    tombstoned_at TIMESTAMPTZ,
    PRIMARY KEY (id)
) PARTITION BY HASH (id);

CREATE TABLE photos_p00 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 0);
CREATE TABLE photos_p01 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 1);
CREATE TABLE photos_p02 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 2);
CREATE TABLE photos_p03 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 3);
CREATE TABLE photos_p04 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 4);
CREATE TABLE photos_p05 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 5);
CREATE TABLE photos_p06 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 6);
CREATE TABLE photos_p07 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 7);
CREATE TABLE photos_p08 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 8);
CREATE TABLE photos_p09 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 9);
CREATE TABLE photos_p10 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 10);
CREATE TABLE photos_p11 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 11);
CREATE TABLE photos_p12 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 12);
CREATE TABLE photos_p13 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 13);
CREATE TABLE photos_p14 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 14);
CREATE TABLE photos_p15 PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER 15);

CREATE INDEX photos_owner_nsid_idx ON photos (owner_nsid);
CREATE INDEX photos_last_update_idx ON photos (last_update);
CREATE INDEX photos_sizes_status_idx ON photos (sizes_status);

-- +goose Down
DROP TABLE photos;
DROP TABLE people;
