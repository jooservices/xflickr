#!/usr/bin/env python3
"""Prove sqlc regeneration detects migration drift the same way make gen-stale does."""

from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from sqlc_stale import run_sqlc_generate, working_tree_is_dirty

REPO_ROOT = Path(__file__).resolve().parents[2]
BACKEND_DIR = REPO_ROOT / "backend"

SQLC_YAML = """\
version: "2"
sql:
  - name: demo
    engine: postgresql
    schema: migrations
    queries: queries
    gen:
      go:
        package: gen
        out: gen
        sql_package: pgx/v5
"""

MIGRATION_V1 = """\
-- +goose Up
CREATE TABLE widgets (
    id BIGINT PRIMARY KEY,
    name TEXT NOT NULL
);
-- +goose Down
DROP TABLE widgets;
"""

MIGRATION_V2 = """\
-- +goose Up
CREATE TABLE widgets (
    id BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT ''
);
-- +goose Down
DROP TABLE widgets;
"""

QUERY = """\
-- name: GetWidget :one
SELECT id, name FROM widgets WHERE id = $1;
"""


class SqlcStaleTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory(prefix="sqlc-stale-")
        self.addCleanup(self.temp_dir.cleanup)
        self.root = Path(self.temp_dir.name)
        (self.root / "migrations").mkdir()
        (self.root / "queries").mkdir()
        (self.root / "gen").mkdir()
        (self.root / "sqlc.yaml").write_text(SQLC_YAML, encoding="utf-8")
        (self.root / "migrations" / "001_widgets.sql").write_text(MIGRATION_V1, encoding="utf-8")
        (self.root / "queries" / "widgets.sql").write_text(QUERY, encoding="utf-8")

    def test_migration_change_makes_sqlc_output_and_stale_check_dirty(self) -> None:
        run_sqlc_generate(BACKEND_DIR, self.root / "sqlc.yaml")
        models = self.root / "gen" / "models.go"
        self.assertTrue(models.is_file())
        baseline = models.read_text(encoding="utf-8")
        self.assertIn("type Widget struct", baseline)
        self.assertNotIn("Note", baseline)

        subprocess.run(["git", "init"], cwd=self.root, check=True, capture_output=True)
        subprocess.run(["git", "add", "."], cwd=self.root, check=True, capture_output=True)
        subprocess.run(
            ["git", "-c", "user.name=xflickr", "-c", "user.email=dev@xflickr.local", "commit", "-m", "baseline"],
            cwd=self.root,
            check=True,
            capture_output=True,
        )
        self.assertFalse(working_tree_is_dirty(self.root))

        (self.root / "migrations" / "001_widgets.sql").write_text(MIGRATION_V2, encoding="utf-8")
        run_sqlc_generate(BACKEND_DIR, self.root / "sqlc.yaml")
        updated = models.read_text(encoding="utf-8")
        self.assertNotEqual(baseline, updated)
        self.assertIn("Note", updated)
        self.assertTrue(working_tree_is_dirty(self.root))


if __name__ == "__main__":
    unittest.main()
