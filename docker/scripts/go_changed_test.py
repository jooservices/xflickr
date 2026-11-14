#!/usr/bin/env python3
"""Tests for go_changed.py."""

from __future__ import annotations

import unittest

import go_changed


class Recorder:
    def __init__(self, statuses: list[int] | None = None) -> None:
        self.calls: list[tuple[list[str], dict[str, str] | None]] = []
        self.statuses = statuses or []

    def __call__(self, command: list[str], env: dict[str, str] | None = None) -> int:
        self.calls.append((command, env))
        return self.statuses.pop(0) if self.statuses else 0


class GoChangedTests(unittest.TestCase):
    def test_packages_keeps_distinct_go_directories_in_order(self) -> None:
        files = ["internal/a/x.go", "README.md", "internal/b/y_test.go", "internal/a/z.go", "main.go"]
        self.assertEqual(["./internal/a", "./internal/b", "./."], go_changed.packages(files))

    def test_unit_runs_go_test_on_the_packages(self) -> None:
        recorder = Recorder()
        self.assertEqual(0, go_changed.main(["unit", "internal/a/x.go"], recorder))
        self.assertEqual([(["go", "test", "-count=1", "./internal/a"], None)], recorder.calls)

    def test_integration_checks_the_database_then_runs_serially_with_the_integration_environment(self) -> None:
        recorder = Recorder()
        self.assertEqual(0, go_changed.main(["integration", "internal/a/x.go"], recorder))
        self.assertEqual(["bash", str(go_changed.VERIFY_DB), "verify-db"], recorder.calls[0][0])
        self.assertEqual((["go", "test", "-count=1", "-p", "1", "./internal/a"], go_changed.INTEGRATION_ENV), recorder.calls[1])

    def test_integration_stops_when_the_database_is_not_ready(self) -> None:
        recorder = Recorder([3])
        self.assertEqual(3, go_changed.main(["integration", "internal/a/x.go"], recorder))
        self.assertEqual(1, len(recorder.calls))

    def test_a_test_failure_status_is_returned(self) -> None:
        self.assertEqual(1, go_changed.main(["unit", "internal/a/x.go"], Recorder([1])))

    def test_no_go_files_is_a_clean_no_op(self) -> None:
        recorder = Recorder()
        self.assertEqual(0, go_changed.main(["unit", "README.md"], recorder))
        self.assertEqual([], recorder.calls)

    def test_a_bad_mode_is_refused(self) -> None:
        self.assertEqual(2, go_changed.main([], Recorder()))
        self.assertEqual(2, go_changed.main(["smoke", "a.go"], Recorder()))

    def test_run_returns_the_exit_status_of_the_command(self) -> None:
        self.assertEqual(0, go_changed.run(["true"]))
        self.assertEqual(3, go_changed.run(["sh", "-c", "exit $CODE"], {"CODE": "3"}))


if __name__ == "__main__":
    unittest.main()
