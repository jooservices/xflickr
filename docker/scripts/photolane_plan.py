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
SELECTOR_PATH = Path("backend/internal/catalog/adapters/postgres/photolane.sql")


def query_sql(root: Path, base_id: int, person: str, owner_prefix: str, email: str) -> str:
    """Return a transaction that seeds, analyzes, explains, and rolls back fixtures."""
    selector = (root / SELECTOR_PATH).read_text(encoding="utf-8")
    return f"""BEGIN;
INSERT INTO people (nsid, username) VALUES ('{person}', '{person}');
INSERT INTO people (nsid, username)
SELECT '{owner_prefix}' || n || '@N00', 'plan'
FROM generate_series(1, {FIXTURE_ROWS // 10}) AS owners(n);
INSERT INTO users (email, password_hash, role) VALUES ('{email}', 'plan', 'user') RETURNING id \\gset
INSERT INTO crawl_interests (user_id, person_nsid, depth, via, starred, selected_until)
VALUES (:'id'::bigint, '{person}', 0, 'plan', true, now() + interval '1 day');
INSERT INTO photos (id, owner_nsid, last_update, visibility, sizes, sizes_status, raw, content_hash)
SELECT {base_id} + n,
       CASE WHEN n <= 20 THEN '{person}'
            ELSE '{owner_prefix}' || (((n - 21) / 10) + 1) || '@N00' END,
       now(), 'public', '[]'::jsonb, 'provisional',
       '{{}}'::jsonb, decode(repeat('ab', 32), 'hex')
FROM generate_series(1, {FIXTURE_ROWS}) AS fixture(n);
COMMIT;
ANALYZE photos;
ANALYZE photo_account_sizes;
ANALYZE crawl_interests;
PREPARE photolane_plan (integer, integer, text[]) AS
{selector.rstrip().rstrip(';')};
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
EXECUTE photolane_plan(100000, 20, ARRAY[]::text[]);
"""


def cleanup_sql(person: str, email: str) -> str:
    """Delete only the synthetic owners and rows created for this measurement."""
    return f"""BEGIN;
DELETE FROM photos WHERE owner_nsid LIKE '{person.rsplit('-', 1)[0]}-%';
DELETE FROM crawl_interests WHERE person_nsid = '{person}';
DELETE FROM people WHERE nsid LIKE '{person.rsplit('-', 1)[0]}-%';
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


def inspect_plan(plan: list[dict[str, Any]]) -> list[str]:
    """Reject scans of photo partitions in the real selector plan."""
    nodes = (node for item in plan for node in plan_nodes(item.get("Plan")))
    photo_sequential_scans: list[str] = []
    for node in nodes:
        relation = str(node.get("Relation Name", ""))
        if relation.startswith("photos") and node.get("Node Type") in {"Seq Scan", "Parallel Seq Scan"}:
            photo_sequential_scans.append(relation)
    return photo_sequential_scans


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
    owner_prefix = f"photo-plan-{nonce}-"
    person = f"{owner_prefix}target@N00"
    email = f"photo-plan-{nonce}@invalid"
    try:
        result = subprocess.run(
            args,
            input=query_sql(root, base_id, person, owner_prefix, email),
            capture_output=True,
            text=True,
            timeout=600,
            env=child_env,
            check=False,
        )
        if result.returncode != 0:
            reason = " ".join(result.stderr.strip().splitlines()[:2])[:500] if result.stderr.strip() else "no details"
            raise RuntimeError(f"psql plan measurement failed with exit code {result.returncode}: {reason}")
        plan = json.loads(result.stdout)
        if not isinstance(plan, list):
            raise RuntimeError("psql returned an unexpected query plan")
        photo_sequential_scans = inspect_plan(plan)
        if photo_sequential_scans:
            raise RuntimeError(
                "photo-lane plan check failed: "
                f"photo_sequential_scans={photo_sequential_scans}"
            )
        nodes = [node for item in plan for node in plan_nodes(item.get("Plan"))]
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
            "execution_ms": plan[0].get("Execution Time"),
            "planning_ms": plan[0].get("Planning Time"),
            "plan": plan_summary,
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
