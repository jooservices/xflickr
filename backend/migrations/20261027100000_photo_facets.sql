-- +goose Up
CREATE TABLE photo_facet_status (
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    facet TEXT NOT NULL CHECK (facet IN ('info', 'exif', 'contexts', 'comments', 'photo_favorites')),
    photo_updated_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, facet)
);

CREATE TABLE photo_info (
    photo_id BIGINT PRIMARY KEY REFERENCES photos (id) ON DELETE CASCADE,
    data JSONB NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE photo_exif (
    photo_id BIGINT PRIMARY KEY REFERENCES photos (id) ON DELETE CASCADE,
    camera TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE photo_contexts (
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    context_type TEXT NOT NULL CHECK (context_type IN ('set', 'pool')),
    context_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, context_type, context_id)
);

CREATE TABLE photo_comments (
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    comment_id TEXT NOT NULL,
    author_nsid TEXT NOT NULL DEFAULT '',
    author_name TEXT NOT NULL DEFAULT '',
    date_created TIMESTAMPTZ,
    permalink TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, comment_id)
);
CREATE INDEX photo_comments_page_idx ON photo_comments (photo_id, date_created DESC, comment_id DESC);

CREATE TABLE photo_favorites (
    photo_id BIGINT NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    person_nsid TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    realname TEXT NOT NULL DEFAULT '',
    fave_date TIMESTAMPTZ,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, person_nsid)
);
CREATE INDEX photo_favorites_page_idx ON photo_favorites (photo_id, fave_date DESC, person_nsid);

INSERT INTO app_settings (key, value, user_overridable) VALUES
    ('photo_info_enabled', 'true'::jsonb, FALSE),
    ('photo_exif_enabled', 'true'::jsonb, FALSE),
    ('photo_contexts_enabled', 'true'::jsonb, FALSE),
    ('photo_comments_enabled', 'false'::jsonb, FALSE),
    ('photo_favorites_enabled', 'false'::jsonb, FALSE);

-- +goose Down
DELETE FROM app_settings WHERE key IN (
    'photo_info_enabled',
    'photo_exif_enabled',
    'photo_contexts_enabled',
    'photo_comments_enabled',
    'photo_favorites_enabled'
);
DROP TABLE photo_favorites;
DROP TABLE photo_comments;
DROP TABLE photo_contexts;
DROP TABLE photo_exif;
DROP TABLE photo_info;
DROP TABLE photo_facet_status;
