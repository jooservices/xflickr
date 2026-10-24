-- +goose Up
CREATE TABLE outbox_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic TEXT NOT NULL,
    key TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX outbox_events_pending_topic_id_idx
    ON outbox_events (topic, id)
    WHERE processed_at IS NULL;

-- +goose Down
DROP TABLE outbox_events;
