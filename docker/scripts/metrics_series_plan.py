#!/usr/bin/env python3
"""Measure the queue-series read plan on a private worktree database."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterator

from worktree_env import parse_env_text

FIXTURE_ROWS = 400_001
SERIES_SQL = """SELECT bucket, value
FROM metric_samples
WHERE name = 'queue.jobs_available' AND labels_hash = :'labels_hash'
  AND bucket >= :'from'::timestamptz AND bucket < :'to'::timestamptz
ORDER BY bucket ASC
LIMIT 60"""


def labels_hash(labels: dict[str, str]) -> str:
    """Return the same sorted-key JSON SHA-256 used by metrics.domain."""
    canonical = json.dumps(labels, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()


def query_sql() -> str:
    """Build a rollback-only fixture and EXPLAIN ANALYZE using the API query."""
    return f"""BEGIN;
INSERT INTO metric_samples (name, labels, labels_hash, node, role, bucket, value)
SELECT 'queue.jobs_available',
       CASE WHEN sample >= {FIXTURE_ROWS - 60}
            THEN jsonb_build_object('queue', :'queue', 'role', 'scheduler', 'node', :'node')
            ELSE jsonb_build_object('queue', :'queue' || '-' || sample::text,
                                    'role', 'scheduler', 'node', :'node' || '-' || sample::text)
       END,
       CASE WHEN sample >= {FIXTURE_ROWS - 60} THEN :'labels_hash'
            ELSE lpad(to_hex(sample), 64, '0')
       END,
       CASE WHEN sample >= {FIXTURE_ROWS - 60} THEN :'node'
            ELSE :'node' || '-' || sample::text
       END,
       'scheduler',
       :'base'::timestamptz + (sample * interval '1 minute'), sample::double precision
FROM generate_series(0, {FIXTURE_ROWS - 1}) AS sequence(sample);
ANALYZE metric_samples;
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
{SERIES_SQL};
ROLLBACK;
"""


def plan_nodes(plan: Any) -> Iterator[dict[str, Any]]:
    """Walk the root plan and all nested PostgreSQL plan nodes."""
    if not isinstance(plan, dict):
        return
    yield plan
    children = plan.get("Plans", [])
    if isinstance(children, list):
        for child in children:
            yield from plan_nodes(child)


def inspect_plan(plan: list[dict[str, Any]]) -> tuple[bool, list[str]]:
    """Report whether the unique sample index is used and any sequential scans."""
    nodes = (node for item in plan for node in plan_nodes(item.get("Plan")))
    index_found = False
    sequential_scans: list[str] = []
    for node in nodes:
        node_type = node.get("Node Type")
        if node_type in {"Seq Scan", "Parallel Seq Scan"}:
            sequential_scans.append(str(node_type))
        if node.get("Index Name") == "metric_samples_unique":
            index_found = True
    return index_found, sequential_scans


def worktree_env(root: Path) -> dict[str, str]:
    """Read database settings only after the repository's worktree check passes."""
    checker = root / "docker/scripts/worktree_env.py"
    result = subprocess.run(
        [sys.executable, str(checker), "--check", "--worktree", str(root)],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError("worktree environment check failed")
    values = parse_env_text((root / ".env").read_text(encoding="utf-8"))
    database = values.get("DB_NAME", "")
    if (
        not database.startswith("xflickr_wt")
        or values.get("DB_DATABASE", database) != database
        or values.get("XFLICKR_WORKTREE_PATH") != str(root.resolve())
    ):
        raise RuntimeError("metrics plan requires this worktree's private database")
    required = ("DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD")
    if any(not values.get(key) for key in required):
        raise RuntimeError("worktree database connection is incomplete")
    values["DB_NAME"] = database
    return values


def measure(root: Path) -> dict[str, Any]:
    """Run the measured plan and roll back all generated fixture rows."""
    values = worktree_env(root)
    psql = shutil.which("psql")
    if psql is None:
        raise RuntimeError("psql is required for the plan measurement")
    now = datetime.now(timezone.utc).replace(second=0, microsecond=0)
    to = now
    start = to - timedelta(minutes=FIXTURE_ROWS)
    from_time = to - timedelta(minutes=60)
    suffix = uuid.uuid4().hex
    labels = {
        "queue": f"metrics-plan-{suffix}",
        "role": "scheduler",
        "node": f"metrics-plan-node-{suffix}",
    }
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
        "--set",
        f"queue={labels['queue']}",
        "--set",
        f"node={labels['node']}",
        "--set",
        f"labels_hash={labels_hash(labels)}",
        "--set",
        f"base={start.isoformat()}",
        "--set",
        f"from={from_time.isoformat()}",
        "--set",
        f"to={to.isoformat()}",
    ]
    child_env = {"PATH": os.environ.get("PATH", ""), "PGPASSWORD": values["DB_PASSWORD"]}
    result = subprocess.run(
        args,
        input=query_sql(),
        capture_output=True,
        text=True,
        timeout=300,
        env=child_env,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(f"psql plan measurement failed with exit code {result.returncode}")
    plan = json.loads(result.stdout)
    if not isinstance(plan, list):
        raise RuntimeError("psql returned an unexpected query plan")
    index_found, sequential_scans = inspect_plan(plan)
    if sequential_scans or not index_found:
        raise RuntimeError(
            "query plan did not use metric_samples_unique without sequential scans: "
            + json.dumps(plan, separators=(",", ":"))
        )
    return {"fixture_rows": FIXTURE_ROWS, "plan": plan}


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
