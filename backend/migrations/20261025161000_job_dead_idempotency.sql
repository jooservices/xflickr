-- +goose Up
DELETE FROM outbox_events AS duplicates
USING outbox_events AS retained
WHERE duplicates.topic = 'job.dead'
  AND retained.topic = duplicates.topic
  AND retained.key = duplicates.key
  AND retained.id < duplicates.id;

CREATE UNIQUE INDEX outbox_events_job_dead_key_idx
    ON outbox_events (topic, key)
    WHERE topic = 'job.dead';

-- +goose Down
DROP INDEX outbox_events_job_dead_key_idx;
