-- +goose Up
CREATE TABLE albums (
    id TEXT PRIMARY KEY,
    owner_nsid TEXT NOT NULL REFERENCES people (nsid),
    primary_photo_id BIGINT REFERENCES photos (id),
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    photos_count BIGINT NOT NULL DEFAULT 0 CHECK (photos_count >= 0),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX albums_owner_updated_idx ON albums (owner_nsid, updated_at DESC, id DESC);

CREATE TABLE album_photos (
    album_id TEXT NOT NULL REFERENCES albums (id) ON DELETE CASCADE,
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (album_id, photo_id)
) PARTITION BY HASH (photo_id);
CREATE TABLE album_photos_p00 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 0);
CREATE TABLE album_photos_p01 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 1);
CREATE TABLE album_photos_p02 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 2);
CREATE TABLE album_photos_p03 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 3);
CREATE TABLE album_photos_p04 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 4);
CREATE TABLE album_photos_p05 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 5);
CREATE TABLE album_photos_p06 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 6);
CREATE TABLE album_photos_p07 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 7);
CREATE TABLE album_photos_p08 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 8);
CREATE TABLE album_photos_p09 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 9);
CREATE TABLE album_photos_p10 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 10);
CREATE TABLE album_photos_p11 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 11);
CREATE TABLE album_photos_p12 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 12);
CREATE TABLE album_photos_p13 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 13);
CREATE TABLE album_photos_p14 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 14);
CREATE TABLE album_photos_p15 PARTITION OF album_photos FOR VALUES WITH (MODULUS 16, REMAINDER 15);
CREATE INDEX album_photos_order_idx ON album_photos (album_id, position, photo_id);

CREATE TABLE favorites (
    person_nsid TEXT NOT NULL REFERENCES people (nsid),
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    fave_date TIMESTAMPTZ NOT NULL,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (person_nsid, photo_id)
) PARTITION BY HASH (person_nsid);
CREATE TABLE favorites_p00 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 0);
CREATE TABLE favorites_p01 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 1);
CREATE TABLE favorites_p02 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 2);
CREATE TABLE favorites_p03 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 3);
CREATE TABLE favorites_p04 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 4);
CREATE TABLE favorites_p05 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 5);
CREATE TABLE favorites_p06 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 6);
CREATE TABLE favorites_p07 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 7);
CREATE TABLE favorites_p08 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 8);
CREATE TABLE favorites_p09 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 9);
CREATE TABLE favorites_p10 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 10);
CREATE TABLE favorites_p11 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 11);
CREATE TABLE favorites_p12 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 12);
CREATE TABLE favorites_p13 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 13);
CREATE TABLE favorites_p14 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 14);
CREATE TABLE favorites_p15 PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER 15);
CREATE INDEX favorites_page_idx ON favorites (person_nsid, fave_date DESC, photo_id DESC);

CREATE TABLE contacts (
    person_nsid TEXT NOT NULL REFERENCES people (nsid),
    contact_nsid TEXT NOT NULL REFERENCES people (nsid),
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (person_nsid, contact_nsid), CHECK (person_nsid <> contact_nsid)
);
CREATE TABLE galleries (
    id TEXT PRIMARY KEY,
    owner_nsid TEXT NOT NULL REFERENCES people (nsid),
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    primary_photo_id BIGINT REFERENCES photos (id),
    photos_count BIGINT NOT NULL DEFAULT 0 CHECK (photos_count >= 0),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX galleries_owner_updated_idx ON galleries (owner_nsid, updated_at DESC, id DESC);
CREATE TABLE gallery_photos (
    gallery_id TEXT NOT NULL REFERENCES galleries (id) ON DELETE CASCADE,
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (gallery_id, photo_id)
);
CREATE TABLE groups (
    nsid TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    members_count BIGINT NOT NULL DEFAULT 0 CHECK (members_count >= 0),
    pool_count BIGINT NOT NULL DEFAULT 0 CHECK (pool_count >= 0),
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE person_groups (
    person_nsid TEXT NOT NULL REFERENCES people (nsid),
    group_nsid TEXT NOT NULL REFERENCES groups (nsid),
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (person_nsid, group_nsid)
);
CREATE TABLE group_pool_photos (
    group_nsid TEXT NOT NULL REFERENCES groups (nsid) ON DELETE CASCADE,
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_nsid, photo_id)
);

-- +goose Down
DROP TABLE group_pool_photos;
DROP TABLE person_groups;
DROP TABLE groups;
DROP TABLE gallery_photos;
DROP TABLE galleries;
DROP TABLE contacts;
DROP TABLE favorites;
DROP TABLE album_photos;
DROP TABLE albums;
