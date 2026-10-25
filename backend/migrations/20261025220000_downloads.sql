-- +goose Up
CREATE TYPE download_kind AS ENUM ('contact', 'album', 'tray');
CREATE TYPE download_item_state AS ENUM ('pending', 'waiting_sizes', 'fetching', 'ready', 'failed');
CREATE TABLE downloads (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind download_kind NOT NULL,
    subject TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX downloads_user_created_idx ON downloads(user_id, created_at DESC);
CREATE TABLE download_items (
    download_id BIGINT NOT NULL REFERENCES downloads(id) ON DELETE CASCADE,
    photo_id BIGINT NOT NULL REFERENCES photos(id) ON DELETE RESTRICT,
    state download_item_state NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    asset_id BIGINT REFERENCES assets(id) ON DELETE SET NULL,
    error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(download_id, photo_id)
);
CREATE INDEX download_items_state_idx ON download_items(download_id, state);

-- +goose Down
DROP TABLE download_items;
DROP TABLE downloads;
DROP TYPE download_item_state;
DROP TYPE download_kind;
