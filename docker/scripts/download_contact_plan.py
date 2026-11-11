#!/usr/bin/env python3
"""Check the contact-download plan on a temporary 400k-photo fixture."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import uuid
from pathlib import Path
from typing import Any

from photolane_plan import (
    inspect_plan,
    parse_plan_documents,
    plan_nodes,
    worktree_env,
)

FIXTURE_ROWS = 400_000
CONTACT_LIMIT = 501
PHOTO_PARTITIONS = 16
REPOSITORY_PATH = Path(
    "backend/internal/catalog/adapters/downloads/postgres/repository.go"
)
INDEX_SUFFIX = "_owner_download_id_idx"
# Allow up to two merge lookahead rows per photo partition around the limit.
MAX_INDEX_ROWS = CONTACT_LIMIT + 2 * PHOTO_PARTITIONS


def contact_query(root: Path, person: str) -> str:
    """Read the contact query from its adapter and bind safe fixture values."""
    source = (root / REPOSITORY_PATH).read_text(encoding="utf-8")
    try:
        contact_case = source.split('case "contact":', 1)[1]
        query_start = contact_case.index("`") + 1
        query_end = contact_case.index("`, userID, subject)", query_start)
    except (IndexError, ValueError) as error:
        raise RuntimeError("download repository has no recognizable contact query") from error
    owner = person.replace("'", "''")
    return contact_case[query_start:query_end].replace("$1", "1").replace(
        "$2", f"'{owner}'"
    )


def fixture_sql(root: Path, base_id: int, person: str) -> str:
    """Create a large owner among other owners and explain the production query."""
    query = contact_query(root, person)
    photo_owner = (
        f"CASE WHEN (n-1)%{PHOTO_PARTITIONS}=0 THEN '{person}' "
        f"ELSE '{person}-other-' || ((n-1)%{PHOTO_PARTITIONS})::text END"
    )
    return f"""BEGIN;
INSERT INTO people (nsid, username)
SELECT CASE WHEN slot=0 THEN '{person}'
            ELSE '{person}-other-' || slot::text END,
       '{person}'
FROM generate_series(0, {PHOTO_PARTITIONS - 1}) AS owners(slot);
INSERT INTO photos (
    id, owner_nsid, last_update, visibility, raw, content_hash)
SELECT {base_id} + n, {photo_owner}, now(), 'public',
       '{{}}'::jsonb, decode(repeat('ab', 32), 'hex')
FROM generate_series(1, {FIXTURE_ROWS}) AS fixture(n);
COMMIT;
ANALYZE photos;
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
{query};
"""


def cleanup_sql(person: str) -> str:
    """Remove only the synthetic owner and its companion fixture owners."""
    return f"""BEGIN;
DELETE FROM photos
WHERE owner_nsid = '{person}' OR owner_nsid LIKE '{person}-other-%';
DELETE FROM people
WHERE nsid = '{person}' OR nsid LIKE '{person}-other-%';
COMMIT;
"""


def contact_download_indexes(plan: list[dict[str, Any]]) -> list[str]:
    """Return the partition indexes used by the contact query."""
    nodes = (node for item in plan for node in plan_nodes(item.get("Plan")))
    return sorted(
        {
            str(node["Index Name"])
            for node in nodes
            if str(node.get("Relation Name", "")).startswith("photos")
            and str(node.get("Index Name", "")).endswith(INDEX_SUFFIX)
        }
    )


def inspect_contact_plan(document: dict[str, Any]) -> dict[str, Any]:
    """Require the owner-ID index and bounded reads without photo table scans."""
    plan = [document]
    sequential_scans = inspect_plan(plan)
    nodes = list(plan_nodes(document.get("Plan")))
    photo_nodes = [
        node
        for node in nodes
        if str(node.get("Relation Name", "")).startswith("photos")
    ]
    indexes = contact_download_indexes(plan)
    index_rows = sum(
        int(node.get("Actual Rows", 0)) * int(node.get("Actual Loops", 1))
        for node in photo_nodes
        if str(node.get("Index Name", "")).endswith(INDEX_SUFFIX)
    )
    if sequential_scans:
        raise RuntimeError(f"contact plan has photo sequential scans: {sequential_scans}")
    if not indexes:
        raise RuntimeError("contact plan did not use the owner-and-ID index")
    if index_rows > MAX_INDEX_ROWS:
        raise RuntimeError(
            f"contact plan read {index_rows} index rows for LIMIT {CONTACT_LIMIT}"
        )
    return {
        "execution_ms": document.get("Execution Time"),
        "planning_ms": document.get("Planning Time"),
        "owner_id_indexes": indexes,
        "index_rows": index_rows,
        "photo_nodes": [
            {
                "node": node.get("Node Type"),
                "relation": node.get("Relation Name"),
                "index": node.get("Index Name"),
                "actual_rows": node.get("Actual Rows"),
                "loops": node.get("Actual Loops"),
                "shared_hit_blocks": node.get("Shared Hit Blocks"),
                "shared_read_blocks": node.get("Shared Read Blocks"),
            }
            for node in photo_nodes
        ],
    }


def measure(root: Path) -> dict[str, Any]:
    """Run EXPLAIN ANALYZE and remove all generated rows afterward."""
    values = worktree_env(root)
    psql = shutil.which("psql")
    if psql is None:
        raise RuntimeError("psql is required for the plan measurement")
    nonce = uuid.uuid4().hex
    base_id = 8_000_000_000_000_000_000
    person = f"contact-plan-{nonce}-target@N00"
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
    try:
        result = subprocess.run(
            args,
            input=fixture_sql(root, base_id, person),
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
                f"psql plan measurement failed with exit code {result.returncode}: {reason}"
            )
        documents = parse_plan_documents(result.stdout)
        if len(documents) != 1 or not isinstance(documents[0], list) or len(documents[0]) != 1:
            raise RuntimeError("psql returned an unexpected query plan")
        summary = inspect_contact_plan(documents[0][0])
        return {"fixture_rows": FIXTURE_ROWS, **summary}
    finally:
        cleaned = subprocess.run(
            args,
            input=cleanup_sql(person),
            capture_output=True,
            text=True,
            timeout=600,
            env=child_env,
            check=False,
        )
        if cleaned.returncode != 0:
            raise RuntimeError("failed to clean the contact-plan fixture")


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
