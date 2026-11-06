#!/usr/bin/env python3
"""Create the isolated environment of one XFlickr worktree on the shared services.

For a worktree id it creates the database xflickr_wt<id> (with pgvector) and
writes a private .env that selects that database, a Redis key prefix and a search
index prefix. The live and the shared test databases can never be selected.
Credentials are read from the shared credential file and are never printed.
"""

from __future__ import annotations

import argparse
import base64
import os
import re
import secrets
import socket
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Protocol

ID_PATTERN = re.compile(r"^[a-z0-9]{1,24}$")
LIVE_DATABASES = frozenset({"xflickr", "xflickr_test"})
SHARED_ENV_NAME = ".env.docker"
REQUIRED_SHARED = (
    "DOCKER_POSTGRES_HOST",
    "DOCKER_POSTGRES_PORT",
    "DOCKER_POSTGRES_USER",
    "DOCKER_POSTGRES_PASSWORD",
    "DOCKER_REDIS_HOST",
    "DOCKER_REDIS_PORT",
    "DOCKER_REDIS_PASSWORD",
    "DOCKER_ELASTIC_HOST",
    "DOCKER_ELASTIC_PORT",
    "DOCKER_ELASTIC_USER",
    "DOCKER_ELASTIC_PASSWORD",
)
# Keys this helper owns; every other key of an existing .env is kept as it is.
MANAGED_KEYS = frozenset(
    {
        "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD",
        "REDIS_HOST", "REDIS_PORT", "REDIS_DB", "REDIS_PASSWORD", "REDIS_PREFIX",
        "SEARCH_URL", "SEARCH_USERNAME", "SEARCH_PASSWORD", "OPENSEARCH_INDEX_PREFIX", "NODE_ID",
    }
)
TEST_REDIS_INDEX = "8"


def parse_env_text(text: str) -> dict[str, str]:
    """Parse KEY=value lines without a shell, so special characters stay literal."""
    values: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key.strip()] = value
    return values


def render_env(values: dict[str, str]) -> str:
    return "".join(f"{key}={value}\n" for key, value in values.items())


def validate_worktree_id(value: str) -> str:
    if not ID_PATTERN.fullmatch(value):
        raise ValueError("worktree id must be 1 to 24 lowercase letters or digits")
    return value


def database_name(worktree_id: str) -> str:
    name = f"xflickr_wt{validate_worktree_id(worktree_id)}"
    if name in LIVE_DATABASES:  # unreachable by construction; kept as a hard guard
        raise ValueError("refusing to select the live or shared test database")
    return name


def build_env(
    example: dict[str, str],
    shared: dict[str, str],
    worktree_id: str,
    existing: dict[str, str],
    master_key: str,
) -> dict[str, str]:
    """Return the .env of a worktree: example, then kept manual keys, then managed keys."""
    for key in REQUIRED_SHARED:
        if not shared.get(key):
            raise KeyError(key)
    env = dict(example)
    env.update({key: value for key, value in existing.items() if key not in MANAGED_KEYS})
    env.update(
        {
            "DB_HOST": shared["DOCKER_POSTGRES_HOST"],
            "DB_PORT": shared["DOCKER_POSTGRES_PORT"],
            "DB_NAME": database_name(worktree_id),
            "DB_USER": shared["DOCKER_POSTGRES_USER"],
            "DB_PASSWORD": shared["DOCKER_POSTGRES_PASSWORD"],
            "REDIS_HOST": shared["DOCKER_REDIS_HOST"],
            "REDIS_PORT": shared["DOCKER_REDIS_PORT"],
            "REDIS_DB": TEST_REDIS_INDEX,
            "REDIS_PASSWORD": shared["DOCKER_REDIS_PASSWORD"],
            "REDIS_PREFIX": f"xflickr_wt{worktree_id}:",
            "SEARCH_URL": f"http://{shared['DOCKER_ELASTIC_HOST']}:{shared['DOCKER_ELASTIC_PORT']}",
            "SEARCH_USERNAME": shared["DOCKER_ELASTIC_USER"],
            "SEARCH_PASSWORD": shared["DOCKER_ELASTIC_PASSWORD"],
            "OPENSEARCH_INDEX_PREFIX": f"xflickr_wt{worktree_id}_",
            "NODE_ID": f"xflickr-wt{worktree_id}",
        }
    )
    if not env.get("XFLICKR_MASTER_KEYS"):
        env["XFLICKR_MASTER_KEYS"] = master_key
        env["XFLICKR_MASTER_KEY_ACTIVE"] = master_key.split(":", 1)[0]
    return env


class DatabaseAdmin(Protocol):
    def exists(self, name: str) -> bool: ...

    def create(self, name: str) -> None: ...


class PsqlAdmin:  # pragma: no cover - exercised by the manual run against PostgreSQL
    """Creates databases through the psql client; the password travels in the environment."""

    def __init__(self, host: str, port: str, user: str, password: str) -> None:
        self._args = ["psql", "-h", host, "-p", port, "-U", user, "-At", "-v", "ON_ERROR_STOP=1"]
        self._env = {**os.environ, "PGPASSWORD": password}

    def _run(self, database: str, sql: str) -> str:
        result = subprocess.run(
            [*self._args, "-d", database, "-c", sql], env=self._env, capture_output=True, text=True, check=False
        )
        if result.returncode != 0:
            raise RuntimeError("psql failed: " + result.stderr.strip().splitlines()[-1][:200])
        return result.stdout.strip()

    def exists(self, name: str) -> bool:
        return self._run("postgres", f"select 1 from pg_database where datname='{name}'") == "1"

    def create(self, name: str) -> None:
        self._run("postgres", f"create database {name}")
        self._run(name, "create extension if not exists vector")


class Provisioner:
    def __init__(self, admin: DatabaseAdmin) -> None:
        self._admin = admin

    def ensure_database(self, worktree_id: str) -> bool:
        """Create the worktree database when missing; report whether it was created."""
        name = database_name(worktree_id)
        if self._admin.exists(name):
            return False
        self._admin.create(name)
        return True


def check_env(root: Path, admin: DatabaseAdmin | None = None, reachable=None) -> list[str]:
    """Return the problems that keep a worktree .env from being usable; empty means ready."""
    env_path = root / ".env"
    if not env_path.is_file():
        return ["a generated worktree .env is required (run worktree_env.py <id>)"]
    env = parse_env_text(env_path.read_text())
    name = env.get("DB_NAME", "")
    if not name.startswith("xflickr_wt") or name in LIVE_DATABASES:
        return ["the .env must select a worktree database xflickr_wt<id>"]
    problems: list[str] = []
    if admin is None:  # pragma: no cover - needs a live PostgreSQL
        admin = PsqlAdmin(env["DB_HOST"], env["DB_PORT"], env["DB_USER"], env["DB_PASSWORD"])
    if reachable is None:  # pragma: no cover - needs live services
        reachable = _tcp_reachable
    try:
        if not admin.exists(name):
            problems.append(f"database {name} does not exist")
    except RuntimeError as error:
        problems.append(f"PostgreSQL is not usable: {str(error)[:100]}")
    if not reachable(env.get("REDIS_HOST", ""), env.get("REDIS_PORT", "")):
        problems.append("Redis is not reachable")
    search = env.get("SEARCH_URL", "").removeprefix("http://").split("/")[0].partition(":")
    if not reachable(search[0], search[2]):
        problems.append("search is not reachable")
    return problems


def _tcp_reachable(host: str, port: str) -> bool:  # pragma: no cover - needs live services
    try:
        with socket.create_connection((host, int(port)), timeout=3):
            return True
    except (OSError, ValueError):
        return False


def _find_shared_env(start: Path) -> Path:
    override = os.environ.get("WORKSPACES_ENV_DOCKER")
    if override:
        return Path(override)
    for directory in (start.resolve(), *start.resolve().parents):
        if (directory / SHARED_ENV_NAME).is_file():
            return directory / SHARED_ENV_NAME
    return Path(SHARED_ENV_NAME)


def _write_private(path: Path, text: str) -> None:
    handle, temp_name = tempfile.mkstemp(dir=path.parent, prefix=".env.")
    try:
        os.fchmod(handle, 0o600)
        with os.fdopen(handle, "w") as stream:
            stream.write(text)
        os.replace(temp_name, path)
    except BaseException:
        Path(temp_name).unlink(missing_ok=True)
        raise


def run(argv: list[str], admin: DatabaseAdmin | None = None, reachable=None) -> int:
    parser = argparse.ArgumentParser(description="Provision the isolated environment of an XFlickr worktree.")
    parser.add_argument("worktree_id", nargs="?", help="lowercase letters and digits, for example the ticket number")
    parser.add_argument("--check", action="store_true", help="verify the existing .env and the services instead of writing")
    parser.add_argument("--worktree", default=".", help="worktree directory that receives .env")
    parser.add_argument("--shared-env", help=f"shared credential file (default: nearest {SHARED_ENV_NAME})")
    args = parser.parse_args(argv)
    if args.check:
        problems = check_env(Path(args.worktree), admin, reachable)
        for problem in problems:
            print(f"FAIL {problem}")
        if not problems:
            print("Shared services and the worktree database are ready.")
        return 1 if problems else 0
    if args.worktree_id is None:
        parser.error("a worktree id is required unless --check is given")
    try:
        worktree_id = validate_worktree_id(args.worktree_id)
    except ValueError as error:
        print(f"error: {error}")
        return 2
    root = Path(args.worktree)
    try:
        shared_path = Path(args.shared_env) if args.shared_env else _find_shared_env(root)
        shared = parse_env_text(shared_path.read_text())
        example = parse_env_text((root / ".env.example").read_text())
        env_path = root / ".env"
        existing = parse_env_text(env_path.read_text()) if env_path.is_file() else {}
        master_key = "k1:" + base64.b64encode(secrets.token_bytes(32)).decode()
        env = build_env(example, shared, worktree_id, existing, master_key)
        if admin is None:
            admin = PsqlAdmin(
                shared["DOCKER_POSTGRES_HOST"], shared["DOCKER_POSTGRES_PORT"],
                shared["DOCKER_POSTGRES_USER"], shared["DOCKER_POSTGRES_PASSWORD"],
            )
        created = Provisioner(admin).ensure_database(worktree_id)
        _write_private(env_path, render_env(env))
    except (OSError, KeyError, RuntimeError) as error:
        print(f"error: {type(error).__name__}: {str(error)[:120]}")
        return 1
    state = "created" if created else "already present"
    print(f"database {env['DB_NAME']} {state}; {env_path} written with mode 600")
    return 0


if __name__ == "__main__":  # pragma: no cover
    sys.exit(run(sys.argv[1:]))
