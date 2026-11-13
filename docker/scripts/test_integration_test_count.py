#!/usr/bin/env python3
"""Tests for counting completed Go integration test cases."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from integration_test_count import count_logs, count_tests


class IntegrationTestCountTest(unittest.TestCase):
    def test_counts_passed_and_failed_test_events_but_not_package_or_skips(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "integration.jsonl"
            log.write_text(
                '{"Action":"pass","Package":"example.test"}\n'
                '{"Action":"pass","Test":"TestOne"}\n'
                '{"Action":"skip","Test":"TestTwo"}\n'
                '{"Action":"fail","Test":"TestThree"}\n',
                encoding="utf-8",
            )
            self.assertEqual(count_tests(log), 2)

    def test_explicit_run_logs_exclude_stale_files_in_the_log_directory(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            current = root / "current.jsonl"
            stale = root / "stale.jsonl"
            current.write_text('{"Action":"pass","Test":"TestCurrent"}\n', encoding="utf-8")
            stale.write_text('{"Action":"pass","Test":"TestStale"}\n', encoding="utf-8")

            self.assertEqual(count_logs([current]), 1)


if __name__ == "__main__":
    unittest.main()
