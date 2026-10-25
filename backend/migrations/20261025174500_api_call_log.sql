-- +goose Up
-- One row per granted Flickr call, written by the quota governor. A rebuild
-- after a Redis loss counts these rows on top of quota_usage, so calls made
-- since the last flush are never forgotten. Rows older than two quota windows
-- are pruned by the flusher.
CREATE TABLE IF NOT EXISTS api_call_log (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key_id BIGINT NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    class TEXT NOT NULL,
    called_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS api_call_log_key_called_idx ON api_call_log (key_id, called_at);
CREATE INDEX IF NOT EXISTS api_call_log_called_idx ON api_call_log (called_at);

-- +goose Down
DROP TABLE api_call_log;
