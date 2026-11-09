-- +goose Up
-- Storage parameters and supporting indexes for JOOXF-370. This migration does
-- not rewrite work_items or river_job heaps; see docs/runbooks/rebuild-work-items.md.

ALTER TABLE work_items SET (
    fillfactor = 70,
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_cost_limit = 2000
);

ALTER TABLE river_job SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_cost_limit = 2000
);

ALTER TABLE people SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE groups SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE crawl_interests SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE outbox_events SET (autovacuum_vacuum_scale_factor = 0.02);

CREATE INDEX IF NOT EXISTS river_job_kind_active_idx
    ON river_job ((args->>'kind'))
    WHERE state IN ('available', 'pending', 'scheduled', 'retryable', 'running');

CREATE INDEX IF NOT EXISTS crawl_interests_person_user_idx
    ON crawl_interests (person_nsid, user_id);

CREATE INDEX IF NOT EXISTS work_items_pending_keyless_idx
    ON work_items (id)
    WHERE state = 'pending' AND key_id IS NULL;

-- Time-invariant sort key used at insert and by set-based rescore. Absolute
-- value is not a minutes-aware score; only relative order matters.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION work_items_static_score(
    p_plan_weight integer,
    p_direct boolean,
    p_starred boolean,
    p_depth integer,
    p_created timestamptz
) RETURNS double precision
LANGUAGE sql
STABLE
AS $$
    SELECT
        COALESCE(p_plan_weight, 1) * (SELECT (value #>> '{}')::double precision FROM app_settings WHERE key = 'score_weight_plan')
        + CASE WHEN p_direct THEN (SELECT (value #>> '{}')::double precision FROM app_settings WHERE key = 'score_weight_direct_action') ELSE 0 END
        + CASE WHEN p_starred THEN (SELECT (value #>> '{}')::double precision FROM app_settings WHERE key = 'score_weight_starred') ELSE 0 END
        + ((SELECT (value #>> '{}')::integer FROM app_settings WHERE key = 'max_depth') - COALESCE(p_depth, 0))
            * (SELECT (value #>> '{}')::double precision FROM app_settings WHERE key = 'score_weight_depth')
        - (SELECT (value #>> '{}')::double precision FROM app_settings WHERE key = 'score_weight_age_minutes')
            * EXTRACT(EPOCH FROM p_created) / 60.0;
$$;
-- +goose StatementEnd

INSERT INTO app_settings (key, value, user_overridable, min, max) VALUES
    ('queue.completed_retention', '86400'::jsonb, FALSE, 60, 604800),
    ('queue.discarded_retention', '604800'::jsonb, FALSE, 3600, 2592000)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM app_settings WHERE key IN ('queue.completed_retention', 'queue.discarded_retention');
DROP FUNCTION IF EXISTS work_items_static_score(integer, boolean, boolean, integer, timestamptz);
DROP INDEX IF EXISTS work_items_pending_keyless_idx;
DROP INDEX IF EXISTS crawl_interests_person_user_idx;
DROP INDEX IF EXISTS river_job_kind_active_idx;
ALTER TABLE outbox_events RESET (autovacuum_vacuum_scale_factor);
ALTER TABLE crawl_interests RESET (autovacuum_vacuum_scale_factor);
ALTER TABLE groups RESET (autovacuum_vacuum_scale_factor);
ALTER TABLE people RESET (autovacuum_vacuum_scale_factor);
ALTER TABLE river_job RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_vacuum_cost_limit
);
ALTER TABLE work_items RESET (
    fillfactor,
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_vacuum_cost_limit
);
