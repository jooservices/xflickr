-- +goose Up
CREATE TABLE contacts_scan_state (
    account_nsid TEXT PRIMARY KEY,
    checkpoint_at BIGINT NOT NULL CHECK (checkpoint_at > 0),
    work_item_id BIGINT NOT NULL CHECK (work_item_id > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE contacts_scan_state;
