#!/usr/bin/env python3
"""Unit tests for the per-package coverage parser and reporter."""

import tempfile
import unittest
from decimal import Decimal
from pathlib import Path

from coverage_gate import evaluate, format_report, parse_exclusions, parse_profile


class CoverageGateTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)
        self.root = Path(self.temp_dir.name)

    def test_profile_aggregates_statement_weighted_package_coverage(self) -> None:
        profile = self.root / "cover.out"
        profile.write_text(
            "mode: set\n"
            "github.com/jooservices/xflickr/internal/alpha/a.go:1.1,1.2 2 1\n"
            "github.com/jooservices/xflickr/internal/alpha/b.go:2.1,2.2 3 0\n"
            "github.com/jooservices/xflickr/internal/beta/c.go:3.1,3.2 4 1\n",
            encoding="utf-8",
        )
        packages = parse_profile(profile)
        self.assertEqual((packages["internal/alpha"].covered, packages["internal/alpha"].total), (2, 5))
        self.assertEqual(packages["internal/beta"].ratio, Decimal("1"))

    def test_exclusion_requires_a_reason_and_must_match_a_package(self) -> None:
        allowlist = self.root / "coverage-exclude.txt"
        allowlist.write_text("cmd/* | command entry points\n", encoding="utf-8")
        exclusions = parse_exclusions(allowlist)
        package = parse_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/cmd/cli/main.go:1.1,1.2 1 0\n",
        )
        measured, excluded, passed = evaluate(package, exclusions, Decimal("85"))
        self.assertEqual(measured, [])
        self.assertEqual(excluded, [("cmd/cli", "command entry points")])
        self.assertTrue(passed)

        allowlist.write_text("internal/generated |   \n", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "requires both"):
            parse_exclusions(allowlist)

        allowlist.write_text("internal/missing | generated code\n", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "matched no package"):
            evaluate(package, parse_exclusions(allowlist), Decimal("85"))

    def test_threshold_controls_pass_fail_and_report_is_coverage_sorted(self) -> None:
        packages = parse_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/internal/below/a.go:1.1,1.2 1 0\n"
            "github.com/jooservices/xflickr/internal/above/a.go:1.1,1.2 6 5\n",
        )
        measured, excluded, passed = evaluate(packages, [], Decimal("85"))
        report = format_report(measured, excluded, Decimal("85"))
        self.assertFalse(passed)
        self.assertLess(report.index("internal/below"), report.index("internal/above"))
        self.assertIn("FAIL", report)
        self.assertIn("PASS", report)

        _measured, _excluded, passed_at_83 = evaluate(
            {"internal/above": packages["internal/above"]}, [], Decimal("83")
        )
        self.assertTrue(passed_at_83)

    def test_invalid_threshold_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "threshold"):
            evaluate({}, [], Decimal("0"))


def parse_profile_from_text(path: Path, entries: str):
    path.write_text("mode: set\n" + entries, encoding="utf-8")
    return parse_profile(path)


if __name__ == "__main__":
    unittest.main()
