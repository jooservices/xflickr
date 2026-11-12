#!/usr/bin/env python3
"""Check the photo-lane selector plan on a temporary 400k-row fixture."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import uuid
from pathlib import Path
from typing import Any, Iterator

from worktree_env import parse_env_text

FIXTURE_ROWS = 400_000
READY_CAP = 200
SELECTOR_PATH = Path("backend/internal/catalog/adapters/postgres/photolane.sql")
CONFIRMED_FRAGMENT_PATH = Path(
    "backend/internal/catalog/adapters/postgres/photo_sizes_confirmed.sql"
)
PHOTO_LANE_ORDER_INDEX_SUFFIX = "_photo_lane_order_idx"
RIVER_ACTIVE_KIND_INDEXES = {
    "river_job_kind_active_idx",
    "river_job_args_kind_active_idx",
}


def query_sql(root: Path, base_id: int, person: str, email: str) -> str:
    """Return a transaction that seeds, analyzes, explains, and rolls back fixtures."""
    selector = (root / SELECTOR_PATH).read_text(encoding="utf-8")
    confirmed_fragment = (root / CONFIRMED_FRAGMENT_PATH).read_text(encoding="utf-8")
    marker = "__PHOTO_SIZES_CONFIRMED__"
    if marker not in selector:
        raise RuntimeError("photo-lane selector has no sizes-confirmation marker")
    selector = selector.replace(marker, confirmed_fragment.strip().rstrip(";"))
    return f"""BEGIN;
INSERT INTO people (nsid, username) VALUES ('{person}', '{person}');
INSERT INTO users (email, password_hash, role)
VALUES ('{email}', 'plan', 'user') RETURNING id \\gset
INSERT INTO crawl_interests (user_id, person_nsid, depth, via, starred, selected_until)
VALUES (:'id'::bigint, '{person}', 0, 'plan', true, now() + interval '1 day');
INSERT INTO photos (
    id, owner_nsid, uploaded_at, last_update, visibility, sizes, sizes_status,
    raw, content_hash)
SELECT {base_id} + n, '{person}', now() - n * interval '1 second', now(),
       'public', '[]'::jsonb, 'provisional',
       '{{}}'::jsonb, decode(repeat('ab', 32), 'hex')
FROM generate_series(1, {FIXTURE_ROWS}) AS fixture(n);
COMMIT;
BEGIN;
INSERT INTO river_job (state, args, kind, queue)
SELECT 'retryable',
       jsonb_build_object(
           'kind', 'photo.sizes',
           'args', jsonb_build_object('dedupe_key', 'photo-plan:{person}'),
           'queue', 'sync'
       ),
       'xflickr_job', 'sync'
FROM generate_series(1, {FIXTURE_ROWS}) AS fixture(n);
COMMIT;
INSERT INTO schedule_meta (key, value)
SELECT 'photo_lane_cursor:{person}',
       jsonb_build_object('uploaded_at', uploaded_at, 'photo_id', id)::text
FROM photos WHERE owner_nsid = '{person}' AND id = {base_id + FIXTURE_ROWS // 2};
ANALYZE photos;
ANALYZE photo_account_sizes;
ANALYZE crawl_interests;
ANALYZE river_job;
PREPARE photolane_plan (integer, integer, text[], integer, integer) AS
{selector.rstrip().rstrip(';')};
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
EXECUTE photolane_plan(1, 20, ARRAY[]::text[], 50, 0);
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT
    EXISTS (SELECT 1 FROM river_job
        WHERE args->>'kind'='photo.sizes'
          AND args->>'queue' IN ('sync','sync-low')
          AND state IN ('available','pending','scheduled','retryable','running')
        LIMIT 1 OFFSET {READY_CAP - 1}),
    EXISTS (SELECT 1 FROM river_job
        WHERE args->>'kind' IN ('photo.sizes','photo.facet')
          AND args->>'queue' IN ('sync','sync-low')
          AND state IN ('available','pending','scheduled','retryable','running')
        LIMIT 1 OFFSET {READY_CAP - 1});
"""


def cleanup_sql(person: str, email: str) -> str:
    """Delete only the synthetic owners and rows created for this measurement."""
    return f"""BEGIN;
DELETE FROM river_job
WHERE args @> jsonb_build_object(
    'kind', 'photo.sizes',
    'args', jsonb_build_object('dedupe_key', 'photo-plan:{person}')
);
DELETE FROM schedule_meta WHERE key = 'photo_lane_cursor:{person}';
DELETE FROM photos WHERE owner_nsid = '{person}';
DELETE FROM crawl_interests WHERE person_nsid = '{person}';
DELETE FROM people WHERE nsid = '{person}';
DELETE FROM users WHERE email = '{email}';
COMMIT;
"""


def plan_nodes(plan: Any) -> Iterator[dict[str, Any]]:
    """Walk a JSON plan node and all nested plan nodes."""
    if not isinstance(plan, dict):
        return
    yield plan
    children = plan.get("Plans", [])
    if isinstance(children, list):
        for child in children:
            yield from plan_nodes(child)


def parse_plan_documents(output: str) -> list[Any]:
    """Decode adjacent JSON values emitted by repeated psql EXPLAIN commands."""
    decoder = json.JSONDecoder()
    documents: list[Any] = []
    offset = 0
    while offset < len(output):
        while offset < len(output) and output[offset].isspace():
            offset += 1
        if offset == len(output):
            break
        document, offset = decoder.raw_decode(output, offset)
        documents.append(document)
    return documents


def inspect_plan(plan: list[dict[str, Any]]) -> list[str]:
    """Reject scans of photo partitions in the real selector plan."""
    nodes = (node for item in plan for node in plan_nodes(item.get("Plan")))
    photo_sequential_scans: list[str] = []
    for node in nodes:
        relation = str(node.get("Relation Name", ""))
        if (
            relation.startswith("photos")
            and node.get("Node Type") in {"Seq Scan", "Parallel Seq Scan"}
        ):
            photo_sequential_scans.append(relation)
    return photo_sequential_scans


def photo_lane_order_indexes(plan: list[dict[str, Any]]) -> list[str]:
    """Return the partition indexes used for the owner's ordered photo page."""
    nodes = (node for item in plan for node in plan_nodes(item.get("Plan")))
    return sorted(
        {
            str(node["Index Name"])
            for node in nodes
            if str(node.get("Relation Name", "")).startswith("photos")
            and str(node.get("Index Name", "")).endswith(PHOTO_LANE_ORDER_INDEX_SUFFIX)
        }
    )


def river_job_indexes(plan: list[dict[str, Any]]) -> list[str]:
    """Return the active-kind indexes used to test the ready cap."""
    nodes = (node for item in plan for node in plan_nodes(item.get("Plan")))
    return sorted(
        {
            str(node["Index Name"])
            for node in nodes
            if node.get("Relation Name") == "river_job" and node.get("Index Name")
        }
    )


def has_active_kind_index(plan: list[dict[str, Any]]) -> bool:
    """Return whether the ready-cap probe uses a known active-kind index."""
    return bool(set(river_job_indexes(plan)) & RIVER_ACTIVE_KIND_INDEXES)


def worktree_env(root: Path) -> dict[str, str]:
    """Read connection settings after verifying the worktree database scope."""
    checker = root / "docker/scripts/worktree_env.py"
    checked = subprocess.run(
        [sys.executable, str(checker), "--check", "--worktree", str(root)],
        capture_output=True,
        text=True,
        check=False,
    )
    if checked.returncode != 0:
        raise RuntimeError("worktree environment check failed")
    values = parse_env_text((root / ".env").read_text(encoding="utf-8"))
    database = values.get("DB_NAME", "")
    if (
        not database.startswith("xflickr_wt")
        or values.get("DB_DATABASE", database) != database
        or values.get("XFLICKR_WORKTREE_PATH") != str(root.resolve())
    ):
        raise RuntimeError("photo-lane plan requires this worktree's private database")
    required = ("DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD")
    if any(not values.get(key) for key in required):
        raise RuntimeError("worktree database connection is incomplete")
    values["DB_NAME"] = database
    return values


def measure(root: Path) -> dict[str, Any]:
    """Run EXPLAIN ANALYZE and remove all generated rows afterward."""
    values = worktree_env(root)
    psql = shutil.which("psql")
    if psql is None:
        raise RuntimeError("psql is required for the plan measurement")
    nonce = uuid.uuid4().hex
    base_id = 8_000_000_000_000_000_000
    args = [
        psql,
        "--no-psqlrc",
        "--quiet",
        "--tuples-only",
        "--no-align",
        "--set=ON_ERROR_STOP=1",
        "--host",
        values["DB_HOST"],
        "--port",
        values["DB_PORT"],
        "--username",
        values["DB_USER"],
        "--dbname",
        values["DB_NAME"],
    ]
    child_env = {"PATH": os.environ.get("PATH", ""), "PGPASSWORD": values["DB_PASSWORD"]}
    person = f"photo-plan-{nonce}-target@N00"
    email = f"photo-plan-{nonce}@invalid"
    try:
        result = subprocess.run(
            args,
            input=query_sql(root, base_id, person, email),
            capture_output=True,
            text=True,
            timeout=600,
            env=child_env,
            check=False,
        )
        if result.returncode != 0:
            reason = (
                " ".join(result.stderr.strip().splitlines()[:2])[:500]
                if result.stderr.strip()
                else "no details"
            )
            raise RuntimeError(
                "psql plan measurement failed with exit code "
                f"{result.returncode}: {reason}"
            )
        plan_results = parse_plan_documents(result.stdout)
        if (
            len(plan_results) != 2
            or any(not isinstance(item, list) or len(item) != 1 for item in plan_results)
        ):
            raise RuntimeError("psql returned an unexpected query plan")
        photo_plan, capacity_plan = (result_item[0] for result_item in plan_results)
        photo_sequential_scans = inspect_plan([photo_plan])
        if photo_sequential_scans:
            raise RuntimeError(
                "photo-lane plan check failed: "
                f"photo_sequential_scans={photo_sequential_scans}"
            )
        order_indexes = photo_lane_order_indexes([photo_plan])
        if not order_indexes:
            raise RuntimeError(
                "photo-lane plan check failed: owner/upload/id index was not used"
            )
        capacity_nodes = [node for node in plan_nodes(capacity_plan.get("Plan"))]
        capacity_sequential_scans = [
            str(node.get("Relation Name"))
            for node in capacity_nodes
            if node.get("Relation Name") == "river_job"
            and node.get("Node Type") in {"Seq Scan", "Parallel Seq Scan"}
        ]
        if capacity_sequential_scans:
            raise RuntimeError(
                "photo ready-cap plan check failed: "
                f"sequential_scans={capacity_sequential_scans}"
            )
        capacity_indexes = river_job_indexes([capacity_plan])
        if not has_active_kind_index([capacity_plan]):
            raise RuntimeError(
                "photo ready-cap plan check failed: active-kind index was not used; "
                f"indexes={capacity_indexes}"
            )
        nodes = [node for node in plan_nodes(photo_plan.get("Plan"))]
        plan_summary = [
            {
                "node": node.get("Node Type"),
                "relation": node.get("Relation Name"),
                "index": node.get("Index Name"),
                "actual_start_ms": node.get("Actual Startup Time"),
                "actual_total_ms": node.get("Actual Total Time"),
                "actual_rows": node.get("Actual Rows"),
                "loops": node.get("Actual Loops"),
                "shared_hit_blocks": node.get("Shared Hit Blocks"),
                "shared_read_blocks": node.get("Shared Read Blocks"),
                "rows_removed_by_filter": node.get("Rows Removed by Filter"),
            }
            for node in nodes
            if node.get("Relation Name", "").startswith(("photos", "crawl_interests"))
        ]
        return {
            "fixture_rows": FIXTURE_ROWS,
            "execution_ms": photo_plan.get("Execution Time"),
            "planning_ms": photo_plan.get("Planning Time"),
            "photo_lane_order_indexes": order_indexes,
            "plan": plan_summary,
            "ready_cap": READY_CAP,
            "ready_cap_execution_ms": capacity_plan.get("Execution Time"),
            "ready_cap_planning_ms": capacity_plan.get("Planning Time"),
            "ready_cap_indexes": capacity_indexes,
            "ready_cap_plan": [
                {
                    "node": node.get("Node Type"),
                    "relation": node.get("Relation Name"),
                    "index": node.get("Index Name"),
                    "actual_rows": node.get("Actual Rows"),
                    "loops": node.get("Actual Loops"),
                    "shared_hit_blocks": node.get("Shared Hit Blocks"),
                    "shared_read_blocks": node.get("Shared Read Blocks"),
                    "rows_removed_by_filter": node.get("Rows Removed by Filter"),
                }
                for node in capacity_nodes
                if node.get("Relation Name") == "river_job"
            ],
        }
    finally:
        cleaned = subprocess.run(
            args,
            input=cleanup_sql(person, email),
            capture_output=True,
            text=True,
            timeout=600,
            env=child_env,
            check=False,
        )
        if cleaned.returncode != 0:
            raise RuntimeError("failed to clean the photo-lane plan fixture")


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    try:
        result = measure(root)
    except (OSError, RuntimeError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
