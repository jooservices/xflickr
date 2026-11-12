"""Tests for the migration version checker and the next-migration allocator."""

from __future__ import annotations

import io
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime
from pathlib import Path

from migration_versions import MIGRATIONS_DIR, build_parser, run_check, run_next


def git(repo: Path, *args: str) -> None:
    """Run git in repo with a fixed identity and no signing."""
    subprocess.run(
        [
            "git",
            "-c", "user.name=Test",
            "-c", "user.email=test@example.com",
            "-c", "commit.gpgsign=false",
            "-C", str(repo),
            *args,
        ],
        check=True,
        capture_output=True,
    )


def check_quietly(directory: Path) -> tuple[int, str]:
    """Run the check and return its exit code and error output."""
    stderr = io.StringIO()
    with redirect_stdout(io.StringIO()), redirect_stderr(stderr):
        code = run_check(directory)
    return code, stderr.getvalue()


class CheckTest(unittest.TestCase):
    """Cover duplicate versions, malformed names and clean trees."""

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.directory = Path(tmp.name)

    def write(self, name: str) -> None:
        (self.directory / name).write_text("-- +goose Up\n", encoding="utf-8")

    def test_two_files_with_one_version_fail_and_both_are_named(self) -> None:
        self.write("20261110000000_add_users.sql")
        self.write("20261110000000_add_roles.sql")

        code, stderr = check_quietly(self.directory)

        self.assertEqual(code, 1)
        self.assertIn("20261110000000_add_users.sql", stderr)
        self.assertIn("20261110000000_add_roles.sql", stderr)

    def test_malformed_filename_fails_and_is_named(self) -> None:
        cases = [
            "20261110000000-add-users.sql",
            "2026111000000_add_users.sql",
            "20261110000000_Add_Users.sql",
        ]
        for name in cases:
            with self.subTest(name=name):
                for old in self.directory.glob("*.sql"):
                    old.unlink()
                self.write(name)

                code, stderr = check_quietly(self.directory)

                self.assertEqual(code, 1)
                self.assertIn(name, stderr)

    def test_clean_tree_passes_and_ignores_go_files(self) -> None:
        self.write("20261110000000_add_users.sql")
        self.write("20261110000100_add_roles.sql")
        (self.directory / "embed.go").write_text("package migrations\n", encoding="utf-8")

        code, stderr = check_quietly(self.directory)

        self.assertEqual(code, 0)
        self.assertEqual(stderr, "")


class NextMigrationTest(unittest.TestCase):
    """Allocate against a temporary repository with develop and a remote branch."""

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Path(tmp.name)
        git(self.repo, "init", "-q", "-b", "develop")
        self.commit("20261110000000_base.sql", "base")
        git(self.repo, "update-ref", "refs/remotes/origin/develop", "HEAD")
        git(self.repo, "checkout", "-q", "-b", "feature")
        self.commit("20261115000000_feature.sql", "feature")
        git(self.repo, "update-ref", "refs/remotes/origin/feature", "HEAD")
        git(self.repo, "checkout", "-q", "develop")
        git(self.repo, "branch", "-D", "feature")

    def commit(self, name: str, message: str) -> None:
        directory = self.repo / MIGRATIONS_DIR
        directory.mkdir(parents=True, exist_ok=True)
        (directory / name).write_text("-- +goose Up\n", encoding="utf-8")
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", message)

    def test_next_is_above_develop_and_a_remote_branch(self) -> None:
        name = run_next(self.repo, "add_thing", datetime(2026, 11, 1, 9, 0, 0))

        self.assertEqual(name, "20261115000001_add_thing.sql")

    def test_next_uses_the_current_time_when_it_is_later(self) -> None:
        name = run_next(self.repo, "add_thing", datetime(2026, 11, 20, 10, 30, 5))

        self.assertEqual(name, "20261120103005_add_thing.sql")

    def test_name_must_be_snake_case(self) -> None:
        parser = build_parser()
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            parser.parse_args(["next", "Add Thing"])


if __name__ == "__main__":
    unittest.main()
