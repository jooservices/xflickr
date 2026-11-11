#!/usr/bin/env python3
"""Check bounded favorites and photo-key plans with a temporary 400k-row fixture."""

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


def plan_sql(rows: int, cursor: bool) -> str:
    """Create session-only tables and explain the favorites and photo-key queries."""
    cursor_clause = ""
    cursor_filter = ""
    scan_rows = "account_favorites"
    account_limit = 102
    observation_plan = ""
    if cursor:
        cursor_clause = """
          AND (favorites.fave_date,favorites.photo_id)
              <= (timestamptz '2020-01-03 00:00:00+00',300000)"""
        cursor_filter = """,
cursor_favorites AS MATERIALIZED (
    SELECT flickr_account_id, photo_id, fave_date, removed_at FROM account_favorites
    WHERE (fave_date,photo_id)<(timestamptz '2020-01-03 00:00:00+00',300000)
       OR ((fave_date,photo_id)=(timestamptz '2020-01-03 00:00:00+00',300000)
           AND flickr_account_id<1)
)"""
        scan_rows = "cursor_favorites"
        account_limit += 1
    else:
        observation_plan = f"""
CREATE TEMP TABLE plan_fixture (user_id bigint, account_id bigint);
WITH new_user AS (
    INSERT INTO users(email,password_hash,role)
    VALUES ('plan-' || txid_current()::text || '@example.invalid','plan','user')
    RETURNING id
), new_key AS (
    INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst)
    SELECT id,'plan','plan-' || id::text,'{{}}',100,10,10 FROM new_user
    RETURNING id,user_id
), new_account AS (
    INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
    SELECT user_id,id,'plan-' || id::text,'plan','{{}}','{{}}','ok' FROM new_key
    RETURNING id,user_id
)
INSERT INTO plan_fixture SELECT user_id,id FROM new_account;

INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id)
SELECT fixture.user_id,fixture.account_id,'photo',item::text
FROM plan_fixture fixture CROSS JOIN generate_series(1,{rows}) AS generated(item);
ANALYZE observations_p00;
CREATE TEMP TABLE plan_target AS
SELECT record_id FROM observations
WHERE record_kind='photo'
  AND tableoid='public.observations_p00'::regclass
  AND user_id=(SELECT user_id FROM plan_fixture)
LIMIT 1;
"""
    return f"""
BEGIN;
{observation_plan}

CREATE TEMP TABLE flickr_accounts (id bigint, nsid text, user_id bigint, status text);
INSERT INTO flickr_accounts VALUES
    (1, 'account-a', {USER_ID}, 'ok'), (2, 'account-b', {USER_ID}, 'ok');

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

CREATE TEMP TABLE photo_enrichments (photo_id bigint, kind text);
INSERT INTO photo_enrichments
SELECT id, 'caption' FROM generate_series(1, {rows}) AS id;
CREATE INDEX photo_enrichments_lookup_idx ON photo_enrichments (photo_id, kind);

ANALYZE flickr_accounts;
ANALYZE photos;
ANALYZE favorites;
ANALYZE photo_enrichments;

EXPLAIN (ANALYZE, BUFFERS)
WITH account_favorites AS MATERIALIZED (
    SELECT account_favorites.flickr_account_id, account_favorites.photo_id,
           account_favorites.fave_date, account_favorites.removed_at
    FROM (SELECT id, nsid FROM flickr_accounts WHERE user_id={USER_ID} AND status<>'revoked') accounts
    CROSS JOIN LATERAL (
        SELECT accounts.id AS flickr_account_id, favorites.photo_id,
               favorites.fave_date, favorites.removed_at
        FROM favorites
        WHERE favorites.person_nsid=accounts.nsid
          {cursor_clause}
        ORDER BY favorites.fave_date DESC, favorites.photo_id DESC, accounts.id DESC
        LIMIT {account_limit}
    ) account_favorites
){cursor_filter}, scan_page AS MATERIALIZED (
    SELECT flickr_account_id, photo_id, fave_date, removed_at FROM {scan_rows}
    ORDER BY fave_date DESC, photo_id DESC, flickr_account_id DESC
    LIMIT 102
)
SELECT flickr_account_id, photo_id, fave_date, removed_at FROM scan_page
ORDER BY fave_date DESC, photo_id DESC, flickr_account_id DESC;

EXPLAIN (ANALYZE, BUFFERS)
SELECT photos.id, photos.owner_nsid, photos.title, photos.description,
       photos.tags, photos.taken_at, photos.uploaded_at, photos.last_update,
       photos.visibility, photos.sizes_status, photos.sizes,
       CASE WHEN EXISTS (
           SELECT 1 FROM photo_enrichments e
           WHERE e.photo_id=photos.id AND e.kind='caption'
       ) THEN 'ready' ELSE 'none' END
FROM photos
WHERE photos.id = ANY(ARRAY(SELECT generate_series(1, 101)::bigint))
  AND photos.tombstoned_at IS NULL;
{f'''EXPLAIN (ANALYZE, BUFFERS)
SELECT user_id, flickr_account_id FROM observations
WHERE record_kind='photo'
  AND record_id=(SELECT record_id FROM plan_target)
  AND user_id=(SELECT user_id FROM plan_fixture);
''' if not cursor else ''}
ROLLBACK;
"""


def run_plan(rows: int, cursor: bool) -> str:
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
        input=plan_sql(rows, cursor),
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


def check_plan(output: str, cursor: bool) -> None:
    """Fail if the bounded per-account keyset scan lacks an index plan."""
    page = "cursor" if cursor else "first-page"
    if re.search(r"Seq Scan\s+on\s+favorites_p\d+\b", output):
        raise RuntimeError(f"favorites {page} query plan contains a sequential scan")
    if not re.search(r"Index Scan using \S+ on favorites_p\d+\b", output):
        raise RuntimeError(f"favorites {page} query plan does not use favorites_page_idx")
    if not re.search(r"Index Cond: .*person_nsid = flickr_accounts\.nsid", output):
        raise RuntimeError("favorites index scan is not bounded by the account")
    bounded_limit = False
    for line in output.splitlines():
        if "Limit" not in line or "actual time=" not in line:
            continue
        metrics = dict(part.split("=", 1) for part in line.split() if "=" in part)
        try:
            expected_rows = 103 if cursor else 102
            bounded_limit = float(metrics["rows"]) == expected_rows and metrics["loops"].rstrip(")") == "2"
        except (KeyError, ValueError):
            continue
        if bounded_limit:
            break
    if not bounded_limit:
        raise RuntimeError(f"favorites {page} query plan did not return a bounded page per account")
    if cursor and not re.search(
        r"Index Cond: .*ROW\(fave_date, photo_id\) <= ROW", output
    ):
        raise RuntimeError("favorites cursor plan does not constrain the index range")


def check_photo_plan(output: str, cursor: bool) -> None:
    """Fail if favorite photo hydration scans the full photo table."""
    page = "cursor" if cursor else "first-page"
    if re.search(r"Seq Scan\s+on photos\b", output):
        raise RuntimeError(f"favorite photo {page} plan contains a sequential scan")
    if not re.search(r"(Index Scan using \S+|Bitmap Heap Scan) on photos\b", output):
        raise RuntimeError(f"favorite photo {page} plan does not use the photo ID index")


def check_observation_plan(output: str) -> None:
    """Fail if the real p00 visibility lookup scans observations."""
    if re.search(r"(?:Parallel )?Seq Scan\s+on observations_p\d+\b", output):
        raise RuntimeError("catalog visibility plan contains a sequential scan on observations")
    if not re.search(
        r"Index Only Scan using observations_p00_kind_record_idx on observations_p00\b",
        output,
    ):
        raise RuntimeError("catalog visibility plan does not use the merged p00 lookup index")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rows", type=int, default=400_000, help="favorites fixture size (minimum 400000)")
    args = parser.parse_args()
    if args.rows < 400_000:
        parser.error("--rows must be at least 400000")

    try:
        print(f"Favorites plan fixture rows: {args.rows}")
        for cursor in (False, True):
            label = "cursor page" if cursor else "first page"
            print(f"Favorites {label} plan:")
            output = run_plan(args.rows, cursor)
            print(output, end="")
            check_plan(output, cursor)
            check_photo_plan(output, cursor)
            if not cursor:
                check_observation_plan(output)
    except (OSError, RuntimeError, ValueError) as error:
        print(f"favorites plan check failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
