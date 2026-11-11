#!/usr/bin/env python3
"""Check the bounded favorites query plan with a temporary 400k-row fixture."""

from __future__ import annotations

import argparse
import importlib.util
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
USER_ID = 72
sys.dont_write_bytecode = True


def database_settings() -> dict[str, str]:
    """Read the worktree connection without exposing its credentials."""
    spec = importlib.util.spec_from_file_location("worktree_env", ROOT / "docker/scripts/worktree_env.py")
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load the worktree environment parser")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    values = module.parse_env_text((ROOT / ".env").read_text())

    if not re.fullmatch(r"xflickr_wt[a-z0-9]{1,24}", values.get("DB_NAME", "")):
        raise RuntimeError("refusing to run outside an isolated XFlickr worktree database")
    if values.get("DB_HOST") not in {"127.0.0.1", "localhost"}:
        raise RuntimeError("refusing a non-local database host")
    for key in ("DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"):
        if not values.get(key):
            raise RuntimeError(f"missing {key} in the worktree environment")
    return values


def plan_sql(rows: int) -> str:
    """Create session-only tables and explain the production favorites query."""
    return f"""
BEGIN;
CREATE TEMP TABLE flickr_accounts (nsid text, user_id bigint, status text);
INSERT INTO flickr_accounts VALUES
    ('account-a', {USER_ID}, 'ok'), ('account-b', {USER_ID}, 'ok');

CREATE TEMP TABLE photos (
    id bigint PRIMARY KEY, owner_nsid text, title text, description text,
    tags text[], taken_at timestamptz, uploaded_at timestamptz,
    last_update timestamptz, visibility text, sizes_status text,
    sizes jsonb, tombstoned_at timestamptz
);
INSERT INTO photos
SELECT id, 'owner', 'photo', '', '{{}}', NULL, NULL, now(), 'public',
       'confirmed', '{{}}'::jsonb, NULL
FROM generate_series(1, {rows}) AS id;

CREATE TEMP TABLE favorites (
    person_nsid text, photo_id bigint, fave_date timestamptz, removed_at timestamptz
 ) PARTITION BY HASH (person_nsid);
DO $$
DECLARE remainder integer;
BEGIN
    FOR remainder IN 0..15 LOOP
        EXECUTE format(
            'CREATE TEMP TABLE favorites_p%s PARTITION OF favorites FOR VALUES WITH (MODULUS 16, REMAINDER %s)',
            lpad(remainder::text, 2, '0'), remainder
        );
    END LOOP;
END $$;
INSERT INTO favorites
SELECT CASE WHEN id % 2 = 0 THEN 'account-a' ELSE 'account-b' END,
       id, timestamptz '2020-01-01 00:00:00+00' + id * interval '1 second', NULL
FROM generate_series(1, {rows}) AS id;
CREATE INDEX favorites_page_idx ON favorites (person_nsid, fave_date DESC, photo_id DESC);

CREATE TEMP TABLE observations (user_id bigint, record_kind text, record_id text);
INSERT INTO observations
SELECT {USER_ID}, 'photo', id::text FROM generate_series(1, {rows}) AS id;
CREATE INDEX observations_photo_idx ON observations (user_id, record_kind, record_id);
CREATE TEMP TABLE sharing_settings (user_id bigint, enabled boolean);
INSERT INTO sharing_settings VALUES ({USER_ID}, true);
CREATE TEMP TABLE photo_enrichments (photo_id bigint, kind text);
INSERT INTO photo_enrichments
SELECT id, 'caption' FROM generate_series(1, {rows}) AS id;
CREATE INDEX photo_enrichments_lookup_idx ON photo_enrichments (photo_id, kind);

ANALYZE flickr_accounts;
ANALYZE photos;
ANALYZE favorites;
ANALYZE observations;
ANALYZE sharing_settings;
ANALYZE photo_enrichments;

SELECT 'Favorites plan fixture rows: ' || count(*) FROM favorites;
EXPLAIN (ANALYZE, BUFFERS)
WITH visible_account_favorites AS MATERIALIZED (
    SELECT account_favorites.*
    FROM (SELECT nsid FROM flickr_accounts WHERE user_id={USER_ID} AND status<>'revoked') accounts
    CROSS JOIN LATERAL (
        SELECT photos.id, photos.owner_nsid, photos.title, photos.description,
               photos.tags, photos.taken_at, photos.uploaded_at, photos.last_update,
               photos.visibility, photos.sizes_status, photos.sizes,
               CASE WHEN EXISTS (
                   SELECT 1 FROM photo_enrichments e
                   WHERE e.photo_id=photos.id AND e.kind='caption'
               ) THEN 'ready' ELSE 'none' END,
               favorites.fave_date
        FROM favorites JOIN photos ON photos.id=favorites.photo_id
        WHERE favorites.person_nsid=accounts.nsid
          AND favorites.removed_at IS NULL
          AND photos.tombstoned_at IS NULL
          AND (
              EXISTS (
                  SELECT 1 FROM observations o
                  WHERE o.user_id={USER_ID} AND o.record_kind='photo'
                    AND o.record_id=photos.id::text
              ) OR (
                  photos.visibility='public' AND EXISTS (
                      SELECT 1 FROM observations o
                      JOIN sharing_settings ss ON ss.user_id=o.user_id
                      WHERE o.record_kind='photo' AND o.record_id=photos.id::text
                        AND ss.enabled
                  )
              )
          )
        ORDER BY favorites.fave_date DESC, photos.id DESC
        LIMIT 101
    ) account_favorites
)
SELECT * FROM visible_account_favorites
ORDER BY fave_date DESC, id DESC
LIMIT 101;
ROLLBACK;
"""


def run_plan(rows: int) -> str:
    """Run psql without putting the worktree password in its arguments."""
    values = database_settings()
    environment = {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "PGPASSWORD": values["DB_PASSWORD"],
        "PGCONNECT_TIMEOUT": "5",
        "PGSSLMODE": "disable",
    }
    result = subprocess.run(
        [
            "psql",
            "-X",
            "--no-password",
            "-v",
            "ON_ERROR_STOP=1",
            "-h",
            values["DB_HOST"],
            "-p",
            values["DB_PORT"],
            "-U",
            values["DB_USER"],
            "-d",
            values["DB_NAME"],
        ],
        input=plan_sql(rows),
        text=True,
        capture_output=True,
        env=environment,
        check=False,
    )
    if result.returncode != 0:
        password = values["DB_PASSWORD"]
        message = result.stderr.replace(password, "[redacted]")
        raise RuntimeError(message.strip() or f"psql exited with {result.returncode}")
    return result.stdout


def check_plan(output: str) -> None:
    """Fail if the per-account page scans all favorites or lacks an index plan."""
    if re.search(r"Seq Scan\s+on\s+favorites_p\d+\b", output):
        raise RuntimeError("favorites query plan contains a sequential scan")
    if not re.search(r"Index Scan using \S+ on favorites_p\d+\b", output):
        raise RuntimeError("favorites query plan does not use favorites_page_idx")
    if "Index Cond: (person_nsid = flickr_accounts.nsid)" not in output:
        raise RuntimeError("favorites index scan is not bounded by the account")
    if not re.search(r"Limit\s+\(.*actual time=.*rows=101(?:\.0+)? loops=2\)", output):
        raise RuntimeError("favorites query plan did not return a bounded page per account")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rows", type=int, default=400_000, help="favorites fixture size (minimum 400000)")
    args = parser.parse_args()
    if args.rows < 400_000:
        parser.error("--rows must be at least 400000")

    try:
        output = run_plan(args.rows)
        print(output, end="")
        check_plan(output)
    except (OSError, RuntimeError, ValueError) as error:
        print(f"favorites plan check failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
