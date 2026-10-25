-- +goose Up
CREATE TYPE river_job_state AS ENUM (
    'available', 'cancelled', 'completed', 'discarded', 'pending',
    'retryable', 'running', 'scheduled'
);

CREATE TABLE river_job (
    id BIGSERIAL PRIMARY KEY,
    state river_job_state NOT NULL DEFAULT 'available',
    attempt SMALLINT NOT NULL DEFAULT 0,
    max_attempts SMALLINT NOT NULL DEFAULT 25,
    attempted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finalized_at TIMESTAMPTZ,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    priority SMALLINT NOT NULL DEFAULT 1,
    args JSONB NOT NULL,
    attempted_by TEXT[],
    errors JSONB[],
    kind TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    queue TEXT NOT NULL DEFAULT 'default',
    tags VARCHAR(255)[] NOT NULL DEFAULT '{}',
    unique_key BYTEA,
    unique_states BIT(8),
    CONSTRAINT river_job_finalized_or_finalized_at_null CHECK
        ((finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded')) OR
         (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded'))),
    CONSTRAINT river_job_max_attempts_positive CHECK (max_attempts > 0),
    CONSTRAINT river_job_priority_in_range CHECK (priority BETWEEN 1 AND 4),
    CONSTRAINT river_job_queue_length CHECK (char_length(queue) > 0 AND char_length(queue) < 128),
    CONSTRAINT river_job_kind_length CHECK (char_length(kind) > 0 AND char_length(kind) < 128)
);

CREATE INDEX river_job_kind_idx ON river_job USING btree(kind);
CREATE INDEX river_job_state_finalized_idx ON river_job USING btree(state, finalized_at) WHERE finalized_at IS NOT NULL;
CREATE INDEX river_job_fetch_idx ON river_job USING btree(state, queue, priority, scheduled_at, id);
CREATE INDEX river_job_args_idx ON river_job USING GIN(args);
CREATE INDEX river_job_metadata_idx ON river_job USING GIN(metadata);

-- +goose StatementBegin
CREATE FUNCTION river_job_state_in_bitmask(bitmask BIT(8), state river_job_state)
RETURNS BOOLEAN LANGUAGE SQL IMMUTABLE AS $$
    SELECT CASE state
        WHEN 'available' THEN get_bit(bitmask, 7)
        WHEN 'cancelled' THEN get_bit(bitmask, 6)
        WHEN 'completed' THEN get_bit(bitmask, 5)
        WHEN 'discarded' THEN get_bit(bitmask, 4)
        WHEN 'pending' THEN get_bit(bitmask, 3)
        WHEN 'retryable' THEN get_bit(bitmask, 2)
        WHEN 'running' THEN get_bit(bitmask, 1)
        WHEN 'scheduled' THEN get_bit(bitmask, 0)
        ELSE 0
    END = 1;
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX river_job_unique_idx ON river_job (unique_key)
    WHERE unique_key IS NOT NULL AND unique_states IS NOT NULL
      AND river_job_state_in_bitmask(unique_states, state);

CREATE TABLE river_queue (
    name TEXT PRIMARY KEY NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    paused_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE river_notification (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload TEXT NOT NULL,
    topic TEXT NOT NULL,
    CONSTRAINT river_notification_topic_length CHECK (length(topic) > 0 AND length(topic) < 128)
);
CREATE INDEX river_notification_created_at_idx ON river_notification (created_at);
CREATE INDEX river_notification_topic_id_idx ON river_notification (topic, id);

CREATE UNLOGGED TABLE river_leader (
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    leader_id TEXT NOT NULL,
    name TEXT PRIMARY KEY DEFAULT 'default',
    CONSTRAINT river_leader_name_length CHECK (name = 'default'),
    CONSTRAINT river_leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

-- +goose StatementBegin
CREATE FUNCTION river_job_notify() RETURNS TRIGGER AS $$
DECLARE payload JSON;
BEGIN
    IF NEW.state = 'available' THEN
        payload = json_build_object('queue', NEW.queue);
        PERFORM pg_notify('river_insert', payload::text);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER river_notify AFTER INSERT ON river_job
    FOR EACH ROW EXECUTE PROCEDURE river_job_notify();

-- +goose Down
DROP TRIGGER river_notify ON river_job;
DROP FUNCTION river_job_notify;
DROP TABLE river_leader;
DROP TABLE river_notification;
DROP TABLE river_queue;
DROP INDEX river_job_unique_idx;
DROP FUNCTION river_job_state_in_bitmask;
DROP TABLE river_job;
DROP TYPE river_job_state;
