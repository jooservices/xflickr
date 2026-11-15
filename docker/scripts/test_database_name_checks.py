#!/usr/bin/env python3
"""The project code never checks what the database is called: it uses the credentials it is given."""

from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
FOLDERS = ("backend", "docker/scripts", "web/e2e", "web/src")
SUFFIXES = {".go", ".py", ".sh", ".ts", ".tsx", ".mjs"}
# The one test that scans integration tests for hard-coded database names has to spell the tree name pattern.
EXEMPT = {
    "backend/internal/platform/db/integration_independence_test.go",
    "docker/scripts/test_database_name_checks.py",
}
FORBIDDEN = (
    re.compile(r"xflickr_wt"),
    re.compile(r"\^wt_"),
    re.compile(r"(HasPrefix|startsWith)\([^)]*[\"']wt_[\"']"),
    re.compile(r"\b(ValidateDatabaseName|isWorktreeDatabase|DATABASE_NAME_PATTERN|LIVE_DATABASES)\b"),
)


class DatabaseNameChecksTest(unittest.TestCase):
    def test_AC3_no_code_checks_the_database_name(self) -> None:
        found: list[str] = []
        for folder in FOLDERS:
            for path in sorted((ROOT / folder).rglob("*")):
                relative = path.relative_to(ROOT).as_posix()
                if path.suffix not in SUFFIXES or relative in EXEMPT or "node_modules" in path.parts:
                    continue
                for number, line in enumerate(path.read_text(encoding="utf-8", errors="replace").splitlines(), 1):
                    if any(pattern.search(line) for pattern in FORBIDDEN):
                        found.append(f"{relative}:{number}: {line.strip()[:90]}")
        self.assertEqual(found, [], "database name checks found:\n" + "\n".join(found))


if __name__ == "__main__":
    unittest.main()
