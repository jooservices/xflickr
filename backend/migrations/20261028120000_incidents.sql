-- +goose Up
CREATE TABLE incidents (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind <> '' AND kind = btrim(kind) AND length(kind) <= 128),
    scope TEXT NOT NULL CHECK (scope <> '' AND scope = btrim(scope) AND length(scope) <= 512),
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    title TEXT NOT NULL CHECK (title <> '' AND title = btrim(title) AND length(title) <= 512),
    detail JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(detail) = 'object'),
    state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'acked', 'closed')),
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    count BIGINT NOT NULL DEFAULT 1 CHECK (count > 0),
    acked_by BIGINT,
    closed_at TIMESTAMPTZ,
    CHECK (first_seen <= last_seen),
    CHECK (state <> 'open' OR acked_by IS NULL),
    CHECK (state <> 'acked' OR acked_by IS NOT NULL),
    CHECK ((state = 'closed') = (closed_at IS NOT NULL))
);

CREATE UNIQUE INDEX incidents_open_kind_scope_uidx
    ON incidents (kind, scope)
    WHERE state = 'open';

CREATE INDEX incidents_state_last_seen_idx
    ON incidents (state, last_seen DESC);

-- +goose Down
DROP TABLE incidents;
