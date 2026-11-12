#!/usr/bin/env python3
"""Measure the contacts account-user lookup on a 400,000-row fixture."""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
ROOT = SCRIPT_DIR.parents[1]
sys.path.insert(0, str(SCRIPT_DIR))

from worktree_env import database_name, parse_env_text  # noqa: E402


def postgres_environment(worktree_id: str) -> dict[str, str]:
    values = parse_env_text((ROOT / ".env").read_text())
    expected = database_name(worktree_id)
    if values.get("DB_NAME") != expected or values.get("DB_DATABASE") != expected:
        raise ValueError("worktree .env does not select the expected database")
    if values.get("XFLICKR_WORKTREE_PATH") != str(ROOT.resolve()):
        raise ValueError("worktree .env belongs to another path")
    required = ("DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD")
    if any(not values.get(key) for key in required):
        raise ValueError("worktree PostgreSQL settings are incomplete")
    return {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "PGHOST": values["DB_HOST"],
        "PGPORT": values["DB_PORT"],
        "PGDATABASE": expected,
        "PGUSER": values["DB_USER"],
        "PGPASSWORD": values["DB_PASSWORD"],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worktree-id", required=True)
    args = parser.parse_args()

    try:
        environment = postgres_environment(args.worktree_id)
    except (OSError, ValueError, KeyError) as error:
        parser.error(str(error))

    result = subprocess.run(
        [
            "psql",
            "-X",
            "-q",
            "-A",
            "-t",
            "-v",
            "ON_ERROR_STOP=1",
            "-f",
            str(SCRIPT_DIR / "contacts_user_lookup_plan.sql"),
        ],
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        print(result.stderr.strip(), file=sys.stderr)
        return result.returncode

    plan = result.stdout.strip()
    if not plan:
        print("PostgreSQL returned no plan", file=sys.stderr)
        return 1
    if "Seq Scan" in plan:
        print(plan, file=sys.stderr)
        print("contacts user lookup used a sequential scan", file=sys.stderr)
        return 1
    print(plan)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
