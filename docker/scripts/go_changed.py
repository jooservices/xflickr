#!/usr/bin/env python3
"""Run the Go tests of the packages that hold the changed files, for the quick round of `scripts/dev/check`.

Usage: go_changed.py unit|integration FILE...   (paths relative to backend/, run from backend/)
"""

from __future__ import annotations

import os
import subprocess
import sys
from collections.abc import Callable, Sequence
from pathlib import Path

MODES = {"unit": ("test", "-count=1"), "integration": ("test", "-count=1", "-p", "1")}
INTEGRATION_ENV = {"XFLICKR_INTEGRATION_TEST": "1", "XFLICKR_DBTEST_INTEGRATION_TEST": "1"}
VERIFY_DB = Path(__file__).resolve().parent / "dev.sh"


def packages(files: Sequence[str]) -> list[str]:
    """The distinct package directories of the Go files, in the order they first appear."""
    found = [f"./{Path(name).parent.as_posix()}" for name in files if name.endswith(".go")]
    return list(dict.fromkeys(found))


def run(command: list[str], env: dict[str, str] | None = None) -> int:
    return subprocess.run(command, env={**os.environ, **(env or {})}, check=False).returncode


def main(argv: Sequence[str], runner: Callable[..., int] = run) -> int:
    if not argv or argv[0] not in MODES:
        print(f"usage: go_changed.py {'|'.join(MODES)} FILE...", file=sys.stderr)
        return 2
    mode, targets = argv[0], packages(argv[1:])
    if not targets:
        print("go_changed: no changed Go files; nothing to test")
        return 0
    if mode == "integration":
        status = runner(["bash", str(VERIFY_DB), "verify-db"])
        if status:
            return status
        return runner(["go", *MODES[mode], *targets], INTEGRATION_ENV)
    return runner(["go", *MODES[mode], *targets])


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
