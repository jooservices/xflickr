#!/usr/bin/env python3
"""Tests for safe recreation of the active worktree database."""

from __future__ import annotations

import unittest
from pathlib import Path

from clean_db import recreate_database


class CleanDatabaseTest(unittest.TestCase):
    def test_recreates_a_worktree_database_then_runs_migrations(self) -> None:
        commands: list[tuple[list[str], dict[str, str]]] = []
        env = {
            "DB_HOST": "127.0.0.1",
            "DB_PORT": "15435",
            "DB_NAME": "xflickr_wtjooxf517",
            "DB_DATABASE": "xflickr_wtjooxf517",
            "DB_USER": "joos",
            "DB_USERNAME": "joos",
            "DB_PASSWORD": "test-only",
            "XFLICKR_WORKTREE_PATH": "/tmp/worktree",
        }

        recreate_database(
            Path("/tmp/worktree"),
            env,
            lambda command, child_env, _cwd: commands.append((command, child_env)) or 0,
        )

        self.assertEqual(len(commands), 4)
        self.assertIn("DROP DATABASE", commands[0][0][-1])
        self.assertIn('"xflickr_wtjooxf517"', commands[0][0][-1])
        self.assertIn("CREATE DATABASE", commands[1][0][-1])
        self.assertIn("CREATE EXTENSION", commands[2][0][-1])
        self.assertEqual(commands[3][0][-5:], ["go", "run", "./cmd/xflickr", "migrate", "up"])
        self.assertEqual(commands[0][1]["PGPASSWORD"], "test-only")

    def test_refuses_live_and_shared_test_databases_before_running_commands(self) -> None:
        for name in ("xflickr", "xflickr_test", "other_database"):
            with self.subTest(name=name):
                calls: list[list[str]] = []
                env = {
                    "DB_HOST": "127.0.0.1",
                    "DB_PORT": "5432",
                    "DB_NAME": name,
                    "DB_USER": "postgres",
                    "DB_PASSWORD": "test-only",
                    "XFLICKR_WORKTREE_PATH": "/tmp/worktree",
                }
                with self.assertRaisesRegex(ValueError, "refusing database"):
                    recreate_database(
                        Path("/tmp/worktree"),
                        env,
                        lambda command, _env, _cwd: calls.append(command) or 0,
                    )
                self.assertEqual(calls, [])

    def test_refuses_an_env_copied_from_another_worktree(self) -> None:
        env = {
            "DB_NAME": "xflickr_wtjooxf517",
            "XFLICKR_WORKTREE_PATH": "/tmp/another-worktree",
        }
        with self.assertRaisesRegex(ValueError, "another worktree"):
            recreate_database(Path("/tmp/worktree"), env, lambda *_: 0)


if __name__ == "__main__":
    unittest.main()
