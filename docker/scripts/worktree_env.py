#!/usr/bin/env python3
"""Map a Workspaces database tree's private credentials to XFlickr settings."""

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

TREE_ENV_NAME = ".env.tree"
LIVE_DATABASES = frozenset({"xflickr", "xflickr_test"})
TEST_REDIS_INDEX = "8"
ID_PATTERN = re.compile(r"^[a-z0-9_-]{2,32}$")
DATABASE_NAME_PATTERN = re.compile(r"^wt_[a-z0-9_]{2,32}$")
SAFE_VALUE = re.compile(r"^[A-Za-z0-9_./:@%+=,~-]*$")
TREE_KEYS = (
    "TREE_POSTGRES_HOST",
    "TREE_POSTGRES_PORT",
    "TREE_POSTGRES_DATABASE",
    "TREE_POSTGRES_USER",
    "TREE_POSTGRES_PASSWORD",
    "TREE_REDIS_HOST",
    "TREE_REDIS_PORT",
    "TREE_REDIS_USER",
    "TREE_REDIS_PASSWORD",
    "TREE_REDIS_PREFIX",
    "TREE_ELASTICSEARCH_HOST",
    "TREE_ELASTICSEARCH_PORT",
    "TREE_ELASTICSEARCH_INDEX_PREFIX",
    "TREE_ELASTICSEARCH_USER",
    "TREE_ELASTICSEARCH_PASSWORD",
)
MANAGED_KEYS = frozenset(
    {
        "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD",
        "XFLICKR_WORKTREE_PATH", "REDIS_HOST", "REDIS_PORT", "REDIS_DB", "REDIS_USERNAME",
        "REDIS_PASSWORD", "REDIS_PREFIX", "SEARCH_URL", "SEARCH_USERNAME", "SEARCH_PASSWORD",
        "OPENSEARCH_INDEX_PREFIX", "NODE_ID",
    }
)


def _unquote(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] == "'":
        return value[1:-1]
    if len(value) >= 2 and value[0] == value[-1] == '"':
        return re.sub(r"\\(.)", lambda match: "\n" if match.group(1) == "n" else match.group(1), value[1:-1])
    return value


def _quote(value: str) -> str:
    """Quote a value so the application's dotenv parser reads it back unchanged."""
    if SAFE_VALUE.fullmatch(value):
        return value
    if "'" not in value and "\n" not in value:
        return f"'{value}'"
    escaped = value.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("\n", "\\n")
    return f'"{escaped}"'


def parse_env_text(text: str) -> dict[str, str]:
    """Parse KEY=value lines without a shell, so special characters stay literal."""
    values: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = _unquote(value.strip())
    return values


def render_env(values: dict[str, str]) -> str:
    return "".join(f"{key}={_quote(value)}\n" for key, value in values.items())


def validate_worktree_id(value: str) -> str:
    if not ID_PATTERN.fullmatch(value):
        raise ValueError("worktree id must be 2 to 32 lowercase letters, digits, hyphens or underscores")
    return value


def database_name(worktree_id: str) -> str:
    """Return the PostgreSQL database resource name produced by scripts/db/tree."""
    return f"wt_{validate_worktree_id(worktree_id).replace('-', '_')}"


def validate_tree(tree: dict[str, str], worktree_id: str) -> None:
    """Require a complete tree for this ticket and its expected private namespaces."""
    worktree_id = validate_worktree_id(worktree_id)
    if tree.get("TREE_NAME") != worktree_id:
        raise ValueError(".env.tree belongs to a different worktree; run scripts/db/tree create <ticket-id>")
    for key in TREE_KEYS:
        if not tree.get(key):
            raise KeyError(key)

    resource = database_name(worktree_id)
    expected = {
        "TREE_POSTGRES_DATABASE": resource,
        "TREE_POSTGRES_USER": resource,
        "TREE_REDIS_USER": resource,
        "TREE_REDIS_PREFIX": f"{resource}:",
        "TREE_ELASTICSEARCH_USER": resource,
        "TREE_ELASTICSEARCH_INDEX_PREFIX": f"{resource}-",
    }
    if any(tree.get(key) != value for key, value in expected.items()):
        raise ValueError(".env.tree resource names do not match the worktree id")
    if tree["TREE_POSTGRES_USER"] == "postgres":
        raise ValueError(".env.tree must contain a non-root PostgreSQL user")


def _search_url(tree: dict[str, str]) -> str:
    return f"http://{tree['TREE_ELASTICSEARCH_HOST']}:{tree['TREE_ELASTICSEARCH_PORT']}"


def _tree_settings(tree: dict[str, str]) -> dict[str, str]:
    return {
        "DB_HOST": tree["TREE_POSTGRES_HOST"],
        "DB_PORT": tree["TREE_POSTGRES_PORT"],
        "DB_NAME": tree["TREE_POSTGRES_DATABASE"],
        "DB_DATABASE": tree["TREE_POSTGRES_DATABASE"],
        "DB_USER": tree["TREE_POSTGRES_USER"],
        "DB_USERNAME": tree["TREE_POSTGRES_USER"],
        "DB_PASSWORD": tree["TREE_POSTGRES_PASSWORD"],
        "REDIS_HOST": tree["TREE_REDIS_HOST"],
        "REDIS_PORT": tree["TREE_REDIS_PORT"],
        "REDIS_USERNAME": tree["TREE_REDIS_USER"],
        "REDIS_PASSWORD": tree["TREE_REDIS_PASSWORD"],
        "REDIS_PREFIX": tree["TREE_REDIS_PREFIX"],
        "SEARCH_URL": _search_url(tree),
        "SEARCH_USERNAME": tree["TREE_ELASTICSEARCH_USER"],
        "SEARCH_PASSWORD": tree["TREE_ELASTICSEARCH_PASSWORD"],
        "OPENSEARCH_INDEX_PREFIX": tree["TREE_ELASTICSEARCH_INDEX_PREFIX"],
    }


def build_env(
    example: dict[str, str],
    tree: dict[str, str],
    worktree_id: str,
    existing: dict[str, str],
    master_key: str,
    worktree_path: str = "",
) -> dict[str, str]:
    """Return XFlickr settings from a tree, keeping manual values and master keys."""
    validate_tree(tree, worktree_id)
    env = dict(example)
    env.update({key: value for key, value in existing.items() if key not in MANAGED_KEYS})
    env.update(_tree_settings(tree))
    env.update(
        {
            "REDIS_DB": TEST_REDIS_INDEX,
            "XFLICKR_WORKTREE_PATH": worktree_path,
            "NODE_ID": f"xflickr-wt{worktree_id}",
        }
    )
    if not env.get("XFLICKR_MASTER_KEYS"):
        env["XFLICKR_MASTER_KEYS"] = master_key
        env["XFLICKR_MASTER_KEY_ACTIVE"] = master_key.split(":", 1)[0]
    return env


class DatabaseConnection(Protocol):
    def check(self, name: str) -> None: ...


class PsqlConnection:
    """Verify the worktree database using its own non-superuser credentials."""

    def __init__(self, host: str, port: str, user: str, password: str) -> None:
        self._args = ["psql", "-h", host, "-p", port, "-U", user, "-At", "-v", "ON_ERROR_STOP=1"]
        self._env = {**os.environ, "PGPASSWORD": password}

    def check(self, name: str) -> None:
        result = subprocess.run(
            [*self._args, "-d", name, "-c", "select current_database()"],
            env=self._env,
            capture_output=True,
            text=True,
            check=False,
        )
        if result.returncode != 0 or result.stdout.strip() != name:
            raise RuntimeError("could not connect to the configured worktree database")


def check_env(root: Path, database: DatabaseConnection | None = None, reachable=None) -> list[str]:
    """Return problems that keep the .env and its private database tree from being usable."""
    env_path = root / ".env"
    tree_path = root / TREE_ENV_NAME
    if not env_path.is_file():
        return [f"a generated .env is required (run worktree_env.py <ticket-id>)"]
    if not tree_path.is_file():
        return [f"{TREE_ENV_NAME} is required; run scripts/db/tree create <ticket-id> first"]

    env = parse_env_text(env_path.read_text())
    tree = parse_env_text(tree_path.read_text())
    try:
        validate_tree(tree, tree.get("TREE_NAME", ""))
    except (KeyError, ValueError) as error:
        return [f"invalid {TREE_ENV_NAME}: {error}"]

    name = env.get("DB_NAME", "")
    alias = env.get("DB_DATABASE", "")
    if name in LIVE_DATABASES or alias in LIVE_DATABASES:
        return ["the .env must not select the live or shared test database"]
    if name != alias:
        return ["the .env database keys must select the same database"]
    user = env.get("DB_USER", "")
    username = env.get("DB_USERNAME", "")
    if user == "postgres" or username == "postgres":
        return ["the .env must not use the PostgreSQL root account postgres"]
    if name != tree["TREE_POSTGRES_DATABASE"]:
        return ["the .env database does not match .env.tree"]
    if user != username:
        return ["the .env PostgreSQL user aliases must match"]
    if user != tree["TREE_POSTGRES_USER"]:
        return ["the .env PostgreSQL user does not match .env.tree"]
    if env.get("XFLICKR_WORKTREE_PATH") != str(root.resolve()):
        return ["this .env was generated for another worktree; run worktree_env.py <ticket-id> here"]

    expected = _tree_settings(tree)
    mismatched = [key for key, value in expected.items() if env.get(key) != value]
    if mismatched:
        return [f"the .env settings do not match .env.tree: {', '.join(mismatched)}"]

    problems: list[str] = []
    if database is None:  # pragma: no cover - needs live PostgreSQL
        database = PsqlConnection(env["DB_HOST"], env["DB_PORT"], env["DB_USER"], env["DB_PASSWORD"])
    if reachable is None:  # pragma: no cover - needs live services
        reachable = _tcp_reachable
    try:
        database.check(name)
    except (OSError, RuntimeError):
        problems.append("PostgreSQL is not usable with the worktree credentials")
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


def _write_private(path: Path, text: str) -> None:
    handle, temporary_path = tempfile.mkstemp(dir=path.parent, prefix=".env.")
    try:
        os.fchmod(handle, 0o600)
        with os.fdopen(handle, "w") as stream:
            stream.write(text)
        os.replace(temporary_path, path)
    except BaseException:
        Path(temporary_path).unlink(missing_ok=True)
        raise


def run(argv: list[str], database: DatabaseConnection | None = None, reachable=None) -> int:
    parser = argparse.ArgumentParser(description="Map the isolated database tree of an XFlickr worktree into .env.")
    parser.add_argument("worktree_id", nargs="?", help="lowercase ticket id used by scripts/db/tree create")
    parser.add_argument("--check", action="store_true", help="verify .env against .env.tree and check shared services")
    parser.add_argument("--worktree", default=".", help="worktree directory that receives .env")
    args = parser.parse_args(argv)
    root = Path(args.worktree)
    if args.check:
        problems = check_env(root, database, reachable)
        for problem in problems:
            print(f"FAIL {problem}")
        if not problems:
            print("The worktree database and shared services are ready.")
        return 1 if problems else 0
    if args.worktree_id is None:
        parser.error("a worktree id is required unless --check is given")
    try:
        worktree_id = validate_worktree_id(args.worktree_id)
        tree_path = root / TREE_ENV_NAME
        if not tree_path.is_file():
            raise FileNotFoundError(f"{TREE_ENV_NAME} is required; run scripts/db/tree create <ticket-id> first")
        tree = parse_env_text(tree_path.read_text())
        validate_tree(tree, worktree_id)
        example = parse_env_text((root / ".env.example").read_text())
        env_path = root / ".env"
        existing = parse_env_text(env_path.read_text()) if env_path.is_file() else {}
        master_key = "k1:" + base64.b64encode(secrets.token_bytes(32)).decode()
        env = build_env(example, tree, worktree_id, existing, master_key, str(root.resolve()))
        _write_private(env_path, render_env(env))
    except (OSError, KeyError, RuntimeError, ValueError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    print(f"worktree .env written with mode 600 for {worktree_id}")
    return 0


if __name__ == "__main__":  # pragma: no cover
    sys.exit(run(sys.argv[1:]))
