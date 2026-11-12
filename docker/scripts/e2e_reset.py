#!/usr/bin/env python3
"""Reset the configured local worktree database for the real-stack E2E."""

from __future__ import annotations

import argparse
import os
import subprocess
from collections.abc import Callable, Sequence


def reset_database(
    host: str,
    port: str,
    username: str,
    database: str,
    runner: Callable[..., object] = subprocess.run,
) -> None:
    """Reset only the worktree database through the application's local endpoint."""
    if host not in {"127.0.0.1", "localhost", "::1"}:
        raise ValueError("E2E database reset requires the configured loopback endpoint")
    if not port.isdigit() or not 1 <= int(port) <= 65535:
        raise ValueError("E2E database reset requires a valid port")
    if not username or not database.startswith("xflickr_wt"):
        raise ValueError("E2E database reset is limited to an XFlickr worktree database")
    if not os.environ.get("PGPASSWORD"):
        raise ValueError("E2E database reset requires the configured database password")

    environment = os.environ.copy()
    environment["PGOPTIONS"] = "-c client_min_messages=warning"
    runner(
        [
            "psql",
            "-X",
            "-h",
            host,
            "-p",
            port,
            "-U",
            username,
            "-d",
            database,
            "-v",
            "ON_ERROR_STOP=1",
            "-q",
            "-c",
            "DROP SCHEMA public CASCADE",
            "-c",
            "CREATE SCHEMA public",
        ],
        check=True,
        env=environment,
        stdout=subprocess.DEVNULL,
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("host")
    parser.add_argument("port")
    parser.add_argument("username")
    parser.add_argument("database")
    args = parser.parse_args(argv)
    reset_database(args.host, args.port, args.username, args.database)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
