#!/usr/bin/env python3
"""Count completed test and subtest events from go test -json output."""

from __future__ import annotations

import json
import argparse
import sys
from pathlib import Path


def count_tests(log: Path) -> int:
    """Count executed test cases, excluding package events and skipped tests."""
    count = 0
    for line_number, line in enumerate(log.read_text(encoding="utf-8").splitlines(), start=1):
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid go test JSON at line {line_number}") from error
        if event.get("Test") and event.get("Action") in {"pass", "fail"}:
            count += 1
    return count


def count_logs(paths: list[Path]) -> int:
    """Sum test events from log files or every JSONL file below directories."""
    logs = sorted({file for path in paths for file in (path.rglob("*.jsonl") if path.is_dir() else [path])})
    if not logs:
        raise ValueError("no integration logs were found")
    return sum(count_tests(log) for log in logs)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--minimum", type=int, default=0)
    parser.add_argument("logs", nargs="+", type=Path)
    args = parser.parse_args()
    try:
        count = count_logs(args.logs)
    except (OSError, ValueError) as error:
        print(f"integration test count failed: {error}", file=sys.stderr)
        return 1
    if count <= args.minimum:
        print(f"integration test count {count} must be greater than {args.minimum}", file=sys.stderr)
        return 1
    print(count)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
