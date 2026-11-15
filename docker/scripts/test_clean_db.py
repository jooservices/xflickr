#!/usr/bin/env python3
"""Tests for resetting the schema of the active worktree database."""

from __future__ import annotations

import unittest
from pathlib import Path

from clean_db import reset_database

ENV = {
    "DB_HOST": "127.0.0.1",
    "DB_PORT": "15435",
    "DB_NAME": "configured_database",
    "DB_DATABASE": "configured_database",
    "DB_USER": "tree_user",
    "DB_USERNAME": "tree_user",
    "DB_PASSWORD": "test-only",
    "XFLICKR_WORKTREE_PATH": "/tmp/worktree",
}


class CleanDatabaseTest(unittest.TestCase):
    def test_resets_the_schema_with_the_configured_account_then_runs_migrations(self) -> None:
        commands: list[tuple[list[str], dict[str, str]]] = []

        reset_database(
            Path("/tmp/worktree"),
            ENV,
            lambda command, child_env, _cwd: commands.append((command, child_env)) or 0,
        )

        self.assertEqual(len(commands), 2)
        psql = commands[0][0]
        self.assertEqual(psql[psql.index("-U") + 1], "tree_user")
        self.assertEqual(psql[psql.index("-d") + 1], "configured_database")
        self.assertIn("DROP SCHEMA public CASCADE", psql)
        self.assertIn("CREATE SCHEMA public", psql)
        self.assertNotIn("CREATE DATABASE", " ".join(psql))
        self.assertEqual(commands[1][0][-5:], ["go", "run", "./cmd/xflickr", "migrate", "up"])
        self.assertEqual(commands[0][1]["PGPASSWORD"], "test-only")

    def test_a_ci_run_needs_no_worktree_path(self) -> None:
        env = {key: value for key, value in ENV.items() if key != "XFLICKR_WORKTREE_PATH"}
        calls: list[list[str]] = []
        reset_database(Path("/tmp/worktree"), {**env, "CI": "true"}, lambda command, _env, _cwd: calls.append(command) or 0)
        self.assertEqual(len(calls), 2)

    def test_refuses_a_missing_or_inconsistent_database_before_running_commands(self) -> None:
        for broken in ({"DB_NAME": ""}, {"DB_NAME": "", "DB_DATABASE": ""}, {"DB_DATABASE": "another"}):
            with self.subTest(broken=broken):
                calls: list[list[str]] = []
                with self.assertRaisesRegex(ValueError, "one configured database"):
                    reset_database(Path("/tmp/worktree"), {**ENV, **broken}, lambda command, _env, _cwd: calls.append(command) or 0)
                self.assertEqual(calls, [])

    def test_refuses_an_incomplete_connection(self) -> None:
        with self.assertRaisesRegex(ValueError, "DB_HOST, DB_PORT and DB_USER"):
            reset_database(Path("/tmp/worktree"), {**ENV, "DB_PORT": "not-a-port"}, lambda *_: 0)

    def test_refuses_an_env_copied_from_another_worktree(self) -> None:
        with self.assertRaisesRegex(ValueError, "another worktree"):
            reset_database(Path("/tmp/worktree"), {**ENV, "XFLICKR_WORKTREE_PATH": "/tmp/another-worktree"}, lambda *_: 0)

    def test_reports_a_failed_command(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "psql failed with exit 3"):
            reset_database(Path("/tmp/worktree"), ENV, lambda *_: 3)


if __name__ == "__main__":
    unittest.main()
