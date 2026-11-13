#!/usr/bin/env python3
"""Tests for counting completed Go integration test cases."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from integration_test_count import count_tests


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


if __name__ == "__main__":
    unittest.main()
