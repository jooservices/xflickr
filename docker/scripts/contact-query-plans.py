#!/usr/bin/env python3
#!/usr/bin/env python3
"""Capture contact-screen query plans against rollback-only 400k-row fixtures."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

from worktree_env import parse_env_text


ROOT = Path(__file__).resolve().parents[2]

SQL = r"""
BEGIN;
CREATE TEMP TABLE photos (
    id BIGINT NOT NULL,
    owner_nsid TEXT NOT NULL,
    title TEXT NOT NULL,
    last_update TIMESTAMPTZ NOT NULL,
    tombstoned_at TIMESTAMPTZ,
    visibility TEXT NOT NULL,
    sizes JSONB NOT NULL,
    sizes_status TEXT NOT NULL,
    sizes_checked_at TIMESTAMPTZ,
    PRIMARY KEY (id)
) PARTITION BY HASH (id);
DO $$
DECLARE partition_no INTEGER;
BEGIN
    FOR partition_no IN 0..15 LOOP
        EXECUTE format(
            'CREATE TEMP TABLE photos_p%s PARTITION OF photos FOR VALUES WITH (MODULUS 16, REMAINDER %s)',
            lpad(partition_no::text, 2, '0'), partition_no
        );
    END LOOP;
END;
$$;
INSERT INTO photos
SELECT g, 'owner-42', 'photo ' || g,
       TIMESTAMPTZ '2025-01-01 00:00:00+00' + g * INTERVAL '1 second',
       NULL, 'public', '[{"url":"https://example.invalid/photo"}]'::jsonb,
       CASE WHEN g <= 800 OR g % 20 = 0 THEN 'none' ELSE 'confirmed' END,
       TIMESTAMPTZ '2026-01-01 00:00:00+00'
FROM generate_series(1, 400000) AS g;
CREATE INDEX photos_owner_recent_idx
    ON photos (owner_nsid, last_update DESC, id DESC)
    WHERE tombstoned_at IS NULL;

CREATE TEMP TABLE observations (
    user_id BIGINT NOT NULL,
    flickr_account_id BIGINT NOT NULL,
    record_kind TEXT NOT NULL,
    record_id TEXT NOT NULL,
    PRIMARY KEY (user_id, flickr_account_id, record_kind, record_id)
) PARTITION BY HASH (record_id);
DO $$
DECLARE partition_no INTEGER;
BEGIN
    FOR partition_no IN 0..15 LOOP
        EXECUTE format(
            'CREATE TEMP TABLE observations_p%s PARTITION OF observations FOR VALUES WITH (MODULUS 16, REMAINDER %s)',
            lpad(partition_no::text, 2, '0'), partition_no
        );
    END LOOP;
END;
$$;
INSERT INTO observations
SELECT 42, 42, 'photo', id::text FROM photos;
CREATE TEMP TABLE sharing_settings (user_id BIGINT PRIMARY KEY, enabled BOOLEAN NOT NULL) ON COMMIT DROP;
INSERT INTO sharing_settings VALUES (42, TRUE);

CREATE TEMP TABLE favorites (
    person_nsid TEXT NOT NULL,
    photo_id BIGINT NOT NULL,
    fave_date TIMESTAMPTZ NOT NULL,
    removed_at TIMESTAMPTZ,
    PRIMARY KEY (person_nsid, photo_id)
) PARTITION BY HASH (person_nsid);
DO $$
DECLARE partition_no INTEGER;
BEGIN
    FOR partition_no IN 0..15 LOOP
        EXECUTE format(
            'CREATE TEMP TABLE favorites_p%s PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER %s)',
            lpad(partition_no::text, 2, '0'), partition_no
        );
    END LOOP;
END;
$$;
INSERT INTO favorites
SELECT 'owner-42', id, last_update, NULL FROM photos;
CREATE INDEX favorites_page_idx ON favorites (person_nsid, fave_date DESC, photo_id DESC)
    WHERE removed_at IS NULL;
CREATE TEMP TABLE people (nsid TEXT PRIMARY KEY, username TEXT, realname TEXT, icon TEXT) ON COMMIT DROP;
INSERT INTO people VALUES ('owner-42', 'owner', 'Owner', '');

CREATE TEMP TABLE albums (
    id BIGINT PRIMARY KEY,
    title TEXT NOT NULL,
    owner_nsid TEXT NOT NULL,
    photos_count BIGINT NOT NULL,
    primary_photo_id BIGINT,
    updated_at TIMESTAMPTZ
) ON COMMIT DROP;
INSERT INTO albums
SELECT g, 'album ' || g, 'owner-42', 10, g, now() - g * INTERVAL '1 minute'
FROM generate_series(1, 101) AS g;
CREATE INDEX albums_contact_order_idx ON albums (owner_nsid, updated_at DESC, id);

CREATE TEMP TABLE work_items (
    id BIGINT PRIMARY KEY,
    kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    user_id BIGINT,
    key_id BIGINT,
    depth INTEGER NOT NULL DEFAULT 0,
    cursor JSONB NOT NULL DEFAULT '{}'::jsonb,
    starred BOOLEAN NOT NULL DEFAULT FALSE,
    score INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    direct_action BOOLEAN NOT NULL DEFAULT FALSE,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    not_before TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    score_updated_at TIMESTAMPTZ,
    UNIQUE (kind, subject_key, user_id)
) ON COMMIT DROP;
INSERT INTO work_items (id, kind, subject_key, user_id, state)
SELECT g, 'photo_sizes', g::text, CASE WHEN g <= 800 THEN 42 ELSE 100 + (g % 999) END, 'pending'
FROM generate_series(1, 400000) AS g;
CREATE INDEX work_items_contact_photo_sizes_idx
    ON work_items (user_id, id) INCLUDE (subject_key)
    WHERE kind = 'photo_sizes'
      AND state IN ('pending', 'ready', 'running', 'waiting')
      AND direct_action = FALSE;

CREATE TEMP TABLE crawl_interests (
    user_id BIGINT NOT NULL,
    person_nsid TEXT NOT NULL,
    depth INTEGER NOT NULL DEFAULT 0,
    starred BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (user_id, person_nsid)
) ON COMMIT DROP;
INSERT INTO crawl_interests VALUES (42, 'owner-42', 1, FALSE);
CREATE TEMP TABLE contact_group_crawl_preferences (
    user_id BIGINT NOT NULL,
    person_nsid TEXT NOT NULL,
    group_nsid TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, person_nsid, group_nsid)
) ON COMMIT DROP;
CREATE TEMP TABLE flickr_accounts (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    status TEXT NOT NULL
) ON COMMIT DROP;
INSERT INTO flickr_accounts VALUES (42, 42, 'ok');
CREATE INDEX flickr_accounts_user_id_idx ON flickr_accounts (user_id, id);

ANALYZE photos;
ANALYZE observations;
ANALYZE favorites;
ANALYZE work_items;

\echo 'Contact photo keyset page'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT ph.id, ph.title, ph.last_update
FROM photos ph
WHERE ph.owner_nsid = 'owner-42' AND ph.tombstoned_at IS NULL
  AND (ph.last_update, ph.id) < (TIMESTAMPTZ '2025-01-06 00:00:00+00', 250000)
  AND (
      EXISTS (SELECT 1 FROM flickr_accounts fa
              CROSS JOIN LATERAL (SELECT 1 FROM observations o
                  WHERE o.user_id = fa.user_id AND o.flickr_account_id = fa.id
                    AND o.record_kind = 'photo' AND o.record_id = ph.id::text LIMIT 1) seen
              WHERE fa.user_id = 42)
      OR (ph.visibility = 'public' AND EXISTS (
          SELECT 1 FROM sharing_settings ss
          JOIN flickr_accounts fa ON fa.user_id = ss.user_id
          CROSS JOIN LATERAL (SELECT 1 FROM observations o
              WHERE o.user_id = ss.user_id AND o.flickr_account_id = fa.id
                AND o.record_kind = 'photo' AND o.record_id = ph.id::text LIMIT 1) seen
          WHERE ss.enabled
      ))
  )
ORDER BY ph.last_update DESC, ph.id DESC LIMIT 101;

\echo 'Contact favorite keyset page'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT f.photo_id, f.fave_date
FROM favorites f JOIN photos ph ON ph.id = f.photo_id
WHERE f.person_nsid = 'owner-42' AND f.removed_at IS NULL
  AND ph.tombstoned_at IS NULL
  AND (f.fave_date, ph.id) < (TIMESTAMPTZ '2025-01-06 00:00:00+00', 250000)
  AND (EXISTS (SELECT 1 FROM flickr_accounts fa
               CROSS JOIN LATERAL (SELECT 1 FROM observations o
                   WHERE o.user_id = fa.user_id AND o.flickr_account_id = fa.id
                     AND o.record_kind = 'photo' AND o.record_id = ph.id::text LIMIT 1) seen
               WHERE fa.user_id = 42)
       OR (ph.visibility = 'public' AND EXISTS (
           SELECT 1 FROM sharing_settings ss
           JOIN flickr_accounts fa ON fa.user_id = ss.user_id
           CROSS JOIN LATERAL (SELECT 1 FROM observations o
               WHERE o.user_id = ss.user_id AND o.flickr_account_id = fa.id
                 AND o.record_kind = 'photo' AND o.record_id = ph.id::text LIMIT 1) seen
           WHERE ss.enabled)))
ORDER BY f.fave_date DESC, ph.id DESC LIMIT 101;

\echo 'Visible contact album cover lookup'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT a.id, (SELECT jsonb_agg(NULLIF(cover.sizes->0->>'url', ''))
              FROM photos cover
              WHERE cover.id = a.primary_photo_id AND cover.tombstoned_at IS NULL
                AND cover.sizes->0->>'url' IS NOT NULL
                AND (EXISTS (SELECT 1 FROM flickr_accounts fa
                             CROSS JOIN LATERAL (SELECT 1 FROM observations o
                                 WHERE o.user_id = fa.user_id AND o.flickr_account_id = fa.id
                                   AND o.record_kind = 'photo' AND o.record_id = cover.id::text LIMIT 1) seen
                             WHERE fa.user_id = 42)
                     OR (cover.visibility = 'public' AND EXISTS (
                         SELECT 1 FROM sharing_settings ss
                         JOIN flickr_accounts fa ON fa.user_id = ss.user_id
                         CROSS JOIN LATERAL (SELECT 1 FROM observations o
                             WHERE o.user_id = ss.user_id AND o.flickr_account_id = fa.id
                               AND o.record_kind = 'photo' AND o.record_id = cover.id::text LIMIT 1) seen
                         WHERE ss.enabled))))
FROM albums a WHERE a.owner_nsid = 'owner-42'
ORDER BY a.updated_at DESC NULLS LAST, a.id LIMIT 101;

\echo 'Bounded photo page summary counts'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
WITH page_ids AS (SELECT unnest(ARRAY(SELECT generate_series(250001, 250500))::bigint[]) AS id)
SELECT count(*) FILTER (WHERE NOT EXISTS (
           SELECT 1 FROM observations o WHERE o.user_id = 42
             AND o.record_kind = 'photo' AND o.record_id = page_ids.id::text))::bigint,
       count(*) FILTER (WHERE ph.tombstoned_at IS NOT NULL)::bigint
FROM page_ids LEFT JOIN photos ph ON ph.id = page_ids.id;

\echo 'Bounded confirmed-size summary count'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
WITH page_ids AS (SELECT unnest(ARRAY(SELECT generate_series(250001, 250500))::bigint[]) AS id)
SELECT count(*)
FROM page_ids JOIN photos ph ON ph.id = page_ids.id
WHERE ph.sizes_status = 'confirmed' AND ph.sizes_checked_at >= ph.last_update;

\echo 'Bounded favorite page summary count'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT count(*) FROM unnest(ARRAY(SELECT generate_series(250001, 250500))::bigint[]) AS page(photo_id)
WHERE NOT EXISTS (
    SELECT 1 FROM favorites f
    WHERE f.person_nsid = 'owner-42' AND f.photo_id = page.photo_id AND f.removed_at IS NULL
);

\echo 'Contact download photo-ID batch'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT p.id, p.owner_nsid, p.visibility, o.user_id, COALESCE(ss.enabled, FALSE)
FROM photos p
LEFT JOIN observations o ON o.record_kind = 'photo' AND o.record_id = p.id::text
LEFT JOIN sharing_settings ss ON ss.user_id = o.user_id
WHERE p.owner_nsid = 'owner-42' AND p.tombstoned_at IS NULL
  AND p.id = ANY(ARRAY(SELECT generate_series(250001, 250500))::bigint[])
  AND EXISTS (SELECT 1 FROM crawl_interests ci
              WHERE ci.user_id = 42 AND ci.person_nsid = 'owner-42')
ORDER BY p.id;

\echo 'Favorite download photo-ID batch'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT p.id, p.owner_nsid, p.visibility, o.user_id, COALESCE(ss.enabled, FALSE)
FROM favorites f
JOIN photos p ON p.id = f.photo_id
LEFT JOIN observations o ON o.record_kind = 'photo' AND o.record_id = p.id::text
LEFT JOIN sharing_settings ss ON ss.user_id = o.user_id
WHERE f.person_nsid = 'owner-42' AND f.removed_at IS NULL
  AND p.tombstoned_at IS NULL
  AND p.id = ANY(ARRAY(SELECT generate_series(250001, 250500))::bigint[])
  AND EXISTS (SELECT 1 FROM crawl_interests ci
              WHERE ci.user_id = 42 AND ci.person_nsid = 'owner-42')
ORDER BY f.fave_date DESC, p.id DESC;

\echo 'Contact size prioritization batch'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
WITH batch AS (
    SELECT wi.id FROM work_items wi
    WHERE wi.user_id = 42 AND wi.kind = 'photo_sizes'
      AND wi.state IN ('pending', 'ready', 'running', 'waiting')
      AND wi.direct_action = FALSE AND EXISTS (
          SELECT 1 FROM photos ph
          WHERE ph.id = CASE WHEN wi.subject_key ~ '^[0-9]+$' THEN wi.subject_key::bigint END
            AND ph.owner_nsid = 'owner-42' AND ph.tombstoned_at IS NULL
            AND ph.sizes_status <> 'confirmed'
      )
    ORDER BY wi.id LIMIT 500 FOR UPDATE OF wi
), updated AS (
    UPDATE work_items wi SET direct_action = TRUE, score_updated_at = NULL, updated_at = now()
    FROM batch WHERE wi.id = batch.id RETURNING wi.id
)
SELECT count(*) FROM updated;

\echo 'Group pool preference enqueue'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
WITH preference AS (
    INSERT INTO contact_group_crawl_preferences
        (user_id, person_nsid, group_nsid, enabled, changed_at)
    VALUES (42, 'owner-42', 'group-42', TRUE, now())
    ON CONFLICT (user_id, person_nsid, group_nsid) DO UPDATE
    SET enabled = EXCLUDED.enabled, changed_at = EXCLUDED.changed_at
    RETURNING enabled
), queued AS (
    INSERT INTO work_items (id, kind, subject_key, user_id, key_id, cursor, state)
    SELECT 400001, 'group_pool', 'group-42', 42,
           (SELECT fa.id FROM flickr_accounts fa
            WHERE fa.user_id = 42 AND fa.status = 'ok' ORDER BY fa.id LIMIT 1),
           '{"page":1,"method":"flickr.groups.pools.getPhotos"}'::jsonb,
           'pending'
    FROM preference WHERE preference.enabled
    ON CONFLICT (kind, subject_key, user_id) DO UPDATE
    SET state = 'pending', cursor = EXCLUDED.cursor, attempts = 0,
        last_error = NULL, not_before = now(), lease_expires_at = NULL,
        created_at = now(), updated_at = now()
    WHERE work_items.state IN ('done', 'failed')
    RETURNING id
)
SELECT enabled FROM preference;

\echo 'Group pool preference gate lookup'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF)
SELECT enabled FROM contact_group_crawl_preferences
WHERE user_id = 42 AND person_nsid = 'owner-42' AND group_nsid = 'group-42';

ROLLBACK;
"""


def main() -> int:
    env_path = ROOT / ".env"
    if not env_path.is_file():
        print("contact-query-plans: missing worktree .env", file=sys.stderr)
        return 2

    config = parse_env_text(env_path.read_text())
    database = config.get("DB_NAME", config.get("DB_DATABASE", ""))
    if not database.startswith("xflickr_wt") or database in {"xflickr", "xflickr_test"}:
        print("contact-query-plans: refusing a non-worktree database", file=sys.stderr)
        return 2
    if config.get("XFLICKR_WORKTREE_PATH") != str(ROOT):
        print("contact-query-plans: .env belongs to a different worktree", file=sys.stderr)
        return 2

    child_env = os.environ.copy()
    child_env["PGPASSWORD"] = config["DB_PASSWORD"]
    command = [
        "psql",
        "-X",
        "-v",
        "ON_ERROR_STOP=1",
        "-h",
        config.get("DB_HOST", "127.0.0.1"),
        "-p",
        config.get("DB_PORT", "15435"),
        "-U",
        config.get("DB_USER", config.get("DB_USERNAME", "")),
        "-d",
        database,
        "-f",
        "-",
    ]
    result = subprocess.run(command, input=SQL, text=True, env=child_env, check=False)
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
