#!/usr/bin/env python3
"""Check goose migration versions and allocate the next free one.

check       fail when two migrations share a version or a filename is malformed
next NAME   print a filename whose version is above every known migration
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MIGRATIONS_DIR = "backend/migrations"
VERSION_FORMAT = "%Y%m%d%H%M%S"
FILE_PATTERN = re.compile(r"(\d{14})_[a-z0-9][a-z0-9_]*\.sql")
NAME_PATTERN = re.compile(r"[a-z0-9][a-z0-9_]*")


def parse_version(filename: str) -> str | None:
    """Return the 14-digit version of a filename matching the pattern, else None."""
    match = FILE_PATTERN.fullmatch(filename)
    return match[1] if match else None


def find_problems(filenames: list[str]) -> list[str]:
    """Describe each malformed filename and each version shared by files."""
    problems: list[str] = []
    owners: dict[str, list[str]] = {}
    for filename in sorted(filenames):
        version = parse_version(filename)
        if version is None:
            problems.append(f"malformed migration filename: {filename}")
        else:
            owners.setdefault(version, []).append(filename)
    for version, files in sorted(owners.items()):
        if len(files) > 1:
            problems.append(f"version {version} is shared by: {', '.join(files)}")
    return problems


def run_check(directory: Path) -> int:
    """Print every problem in the migration directory; return 1 when any exist."""
    filenames = [path.name for path in directory.glob("*.sql")]
    problems = find_problems(filenames)
    for problem in problems:
        print(problem, file=sys.stderr)
    if not problems:
        print(f"migration versions OK ({len(filenames)} files)")
    return 1 if problems else 0


def git_output(repo: Path, *args: str) -> str:
    """Run a read-only git command in repo and return its stdout."""
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def branch_filenames(repo: Path) -> list[str]:
    """List migration filenames in HEAD and in every local or fetched branch."""
    refs = git_output(
        repo, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes/origin"
    ).split()
    filenames: list[str] = []
    for ref in ["HEAD", *refs]:
        listing = git_output(repo, "ls-tree", "-r", "--name-only", ref, "--", MIGRATIONS_DIR)
        filenames.extend(Path(line).name for line in listing.splitlines())
    return filenames


def latest_version(filenames: list[str]) -> str | None:
    """Return the highest valid version among filenames, or None."""
    versions = [version for name in filenames if (version := parse_version(name))]
    return max(versions, default=None)


def next_filename(latest: str | None, name: str, now: datetime) -> str:
    """Return a filename at the current time, or one above latest if that is not later."""
    version = now.strftime(VERSION_FORMAT)
    if latest is not None and version <= latest:
        version = str(int(latest) + 1)
    return f"{version}_{name}.sql"


def run_next(repo: Path, name: str, now: datetime) -> str:
    """Return the next free migration filename for repo and name."""
    working_tree = [path.name for path in (repo / MIGRATIONS_DIR).glob("*.sql")]
    latest = latest_version(working_tree + branch_filenames(repo))
    return next_filename(latest, name, now)


def migration_name(value: str) -> str:
    """Accept only lowercase snake_case names for new migrations."""
    if NAME_PATTERN.fullmatch(value) is None:
        raise argparse.ArgumentTypeError(f"name must be lowercase snake_case: {value!r}")
    return value


def build_parser() -> argparse.ArgumentParser:
    """Build the command-line parser for the check and next commands."""
    parser = argparse.ArgumentParser(description="Check and allocate goose migration versions.")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("check", help="fail on duplicate versions or malformed filenames")
    allocate = commands.add_parser("next", help="print the next free migration filename")
    allocate.add_argument("name", type=migration_name, help="snake_case migration name")
    return parser


def fetch_origin(repo: Path) -> None:
    """Refresh origin so the allocator sees the current develop and PR branches."""
    subprocess.run(["git", "-C", str(repo), "fetch", "--quiet", "origin"], check=True)


def main(argv: list[str] | None = None) -> int:
    """Run the requested command and return its exit status."""
    args = build_parser().parse_args(argv)
    if args.command == "check":
        return run_check(ROOT / MIGRATIONS_DIR)
    try:
        fetch_origin(ROOT)
    except subprocess.CalledProcessError as error:
        print(f"git fetch origin failed with exit {error.returncode}", file=sys.stderr)
        return 1
    print(run_next(ROOT, args.name, datetime.now(timezone.utc).replace(tzinfo=None)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
