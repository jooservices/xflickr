-- +goose Up
ALTER TABLE api_call_log
    ADD COLUMN flickr_account TEXT,
    ADD COLUMN facet TEXT,
    ADD COLUMN attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    ADD COLUMN started_at TIMESTAMPTZ,
    ADD COLUMN http_status SMALLINT CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
    ADD COLUMN flickr_stat TEXT CHECK (flickr_stat IS NULL OR flickr_stat IN ('ok', 'fail')),
    ADD COLUMN flickr_error_code BIGINT,
    ADD COLUMN error_message TEXT CHECK (error_message IS NULL OR char_length(error_message) <= 512),
    ADD COLUMN response_bytes BIGINT CHECK (response_bytes IS NULL OR response_bytes >= 0),
    ADD COLUMN node_id TEXT,
    ADD COLUMN request_id UUID,
    ADD COLUMN outcome TEXT CHECK (outcome IS NULL OR outcome IN ('success', 'failure', 'throttled', 'quota_wait'));

CREATE INDEX api_call_log_outcome_at_idx ON api_call_log (outcome, called_at DESC) WHERE outcome IS NOT NULL;
CREATE INDEX api_call_log_request_id_idx ON api_call_log (request_id) WHERE request_id IS NOT NULL;

-- +goose Down
DROP INDEX api_call_log_request_id_idx;
DROP INDEX api_call_log_outcome_at_idx;
ALTER TABLE api_call_log
    DROP COLUMN outcome,
    DROP COLUMN request_id,
    DROP COLUMN node_id,
    DROP COLUMN response_bytes,
    DROP COLUMN error_message,
    DROP COLUMN flickr_error_code,
    DROP COLUMN flickr_stat,
    DROP COLUMN http_status,
    DROP COLUMN started_at,
    DROP COLUMN attempt,
    DROP COLUMN facet,
    DROP COLUMN flickr_account;
