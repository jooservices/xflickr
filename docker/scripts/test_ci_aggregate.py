#!/usr/bin/env python3
"""Tests for required-job aggregation and valid path-filter skips."""

from __future__ import annotations

import unittest

from ci_aggregate import aggregate, required_jobs


class CIAggregatorTest(unittest.TestCase):
    def test_all_success_and_legitimate_skips_pass(self) -> None:
        result = aggregate(
            {
                "static": "success",
                "go-lint": "skipped",
                "web-lint": "success",
                "integration": "skipped",
            },
            {"static", "web-lint"},
            {"go-lint", "integration"},
        )
        self.assertEqual(result, [])

    def test_failure_cancellation_and_unexpected_skip_fail(self) -> None:
        cases = (
            ({"lint": "failure"}, {"lint"}, "failure"),
            ({"lint": "cancelled"}, {"lint"}, "cancelled"),
            ({"lint": "skipped"}, {"lint"}, "skipped"),
            ({"lint": "success"}, {"lint", "web"}, "missing"),
        )
        for jobs, required, expected in cases:
            with self.subTest(expected=expected):
                failures = aggregate(jobs, required, set())
                self.assertTrue(any(expected in failure for failure in failures))

    def test_skipped_required_job_is_not_made_valid_by_other_optional_skips(self) -> None:
        failures = aggregate(
            {"static": "success", "web": "skipped"},
            {"static", "web"},
            {"integration"},
        )
        self.assertTrue(any("web" in failure and "skipped" in failure for failure in failures))

    def test_path_outputs_select_only_relevant_jobs(self) -> None:
        self.assertEqual(
            required_jobs({"go": "false", "web": "true", "security_web": "true"}),
            {"changes", "static", "web-lint", "web-test", "web-coverage", "security-web"},
        )


if __name__ == "__main__":
    unittest.main()
