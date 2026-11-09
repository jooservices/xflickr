-- +goose Up
CREATE TABLE schedule_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE schedule_meta;
