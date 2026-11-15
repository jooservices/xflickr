#!/usr/bin/env python3
"""Reset the schema of the configured worktree database and apply its migrations."""

from __future__ import annotations

import os
import subprocess
import sys
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path

from worktree_env import parse_env_text

ROOT = Path(__file__).resolve().parents[2]
Runner = Callable[[Sequence[str], Mapping[str, str], Path], int]


def connection_fields(root: Path, env: Mapping[str, str]) -> tuple[str, str, str, str]:
    """Return the database, host, port and user of the .env, which must belong to this worktree."""
    name = env.get("DB_NAME") or env.get("DB_DATABASE", "")
    aliases = [env.get(key, name) for key in ("DB_NAME", "DB_DATABASE")]
    if not name or any(alias != name for alias in aliases):
        raise ValueError("clean-db needs one configured database: DB_NAME and DB_DATABASE must match")
    if env.get("CI", "").lower() != "true" and env.get("XFLICKR_WORKTREE_PATH") != str(root.resolve()):
        raise ValueError("refusing database: .env belongs to another worktree")
    host, port, user = (env.get(key, "") for key in ("DB_HOST", "DB_PORT", "DB_USER"))
    if not host or not port.isdigit() or not user:
        raise ValueError("refusing database: DB_HOST, DB_PORT and DB_USER are required")
    return name, host, port, user


def reset_database(root: Path, env: Mapping[str, str], runner: Runner) -> None:
    """Empty the public schema of the configured database with its own account, then migrate it."""
    name, host, port, user = connection_fields(root, env)
    child_env = {**os.environ, **env, "PGPASSWORD": env.get("DB_PASSWORD", "")}
    psql = [
        "psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "-h", host,
        "-p", port, "-U", user, "-d", name,
    ]
    run_checked(runner, [*psql, "-c", "DROP SCHEMA public CASCADE", "-c", "CREATE SCHEMA public"], child_env, root)
    runtime = root / "docker/scripts/run.sh"
    run_checked(
        runner,
        [str(runtime), "go", "go", "run", "./cmd/xflickr", "migrate", "up"],
        child_env,
        root / "backend",
    )


def run_checked(runner: Runner, command: Sequence[str], env: Mapping[str, str], cwd: Path) -> None:
    result = runner(command, env, cwd)
    if result != 0:
        executable = "xflickr migrate" if "migrate" in command else "psql"
        raise RuntimeError(f"clean-db: {executable} failed with exit {result}")


def main() -> int:
    root = ROOT.resolve()
    env: dict[str, str] = {}
    dotenv = root / ".env"
    try:
        if dotenv.is_file():
            env.update(parse_env_text(dotenv.read_text(encoding="utf-8")))
        env.update(os.environ)
        reset_database(root, env, lambda command, child_env, cwd: subprocess.run(command, cwd=cwd, env=child_env, check=False).returncode)
    except (OSError, ValueError, RuntimeError) as error:
        print(f"clean-db failed: {error}", file=sys.stderr)
        return 1
    print("Worktree database reset and migrations applied.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
