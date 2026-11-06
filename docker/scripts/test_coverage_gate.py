#!/usr/bin/env python3
"""Unit tests for the per-package and per-file coverage gates."""

import subprocess
import tempfile
import unittest
from decimal import Decimal
from pathlib import Path
from unittest import mock

from coverage_gate import (
    baseline_candidates,
    baseline_reached,
    changed_go_files,
    evaluate,
    evaluate_file_coverage,
    format_baseline,
    format_file_report,
    format_report,
    is_excluded_file,
    is_generated_file,
    main,
    parse_baseline,
    parse_exclusions,
    parse_file_profile,
    parse_profile,
)


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

    def test_file_profile_merges_duplicate_blocks(self) -> None:
        files = parse_file_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/internal/alpha/a.go:1.1,1.2 2 0\n"
            "github.com/jooservices/xflickr/internal/alpha/a.go:1.1,1.2 2 1\n"
            "github.com/jooservices/xflickr/internal/alpha/a.go:2.1,2.2 3 0\n",
        )
        self.assertEqual((files["internal/alpha/a.go"].covered, files["internal/alpha/a.go"].total), (2, 5))

    def test_file_gate_below_at_and_above_threshold(self) -> None:
        files = parse_file_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/internal/below/a.go:1.1,1.2 1 0\n"
            "github.com/jooservices/xflickr/internal/at/a.go:1.1,1.2 17 1\n"
            "github.com/jooservices/xflickr/internal/at/a.go:2.1,2.2 3 0\n"
            "github.com/jooservices/xflickr/internal/above/a.go:1.1,1.2 10 1\n",
        )
        results, reached, passed = evaluate_file_coverage(
            files,
            ["backend/internal/below/a.go", "backend/internal/at/a.go", "backend/internal/above/a.go"],
            Decimal("85"),
            {},
            self.root,
        )
        self.assertFalse(passed)
        self.assertEqual(reached, [])
        report = format_file_report(results, Decimal("85"), reached)
        self.assertIn("Failing files: internal/below/a.go", report)
        statuses = {coverage.file: status for coverage, status in results}
        self.assertEqual(statuses["internal/below/a.go"], "FAIL")
        self.assertEqual(statuses["internal/at/a.go"], "PASS")
        self.assertEqual(statuses["internal/above/a.go"], "PASS")

    def test_file_gate_missing_existing_file_fails_and_deleted_is_skipped(self) -> None:
        (self.root / "backend" / "internal" / "x").mkdir(parents=True)
        (self.root / "backend" / "internal" / "x" / "a.go").write_text(
            "package x\n\nfunc Work() {}\n", encoding="utf-8"
        )
        results, _reached, passed = evaluate_file_coverage(
            {}, ["backend/internal/x/a.go", "backend/internal/x/gone.go"], Decimal("85"), {}, self.root
        )
        self.assertFalse(passed)
        self.assertEqual([coverage.file for coverage, _status in results], ["internal/x/a.go"])

    def test_file_gate_missing_file_with_only_types_passes_as_no_statements(self) -> None:
        (self.root / "backend" / "internal" / "x").mkdir(parents=True)
        (self.root / "backend" / "internal" / "x" / "types.go").write_text(
            "package x\n\n// func Ignored() {}\n\nconst Answer = 42\n\ntype Config struct {\n\tName string\n}\n",
            encoding="utf-8",
        )
        results, reached, passed = evaluate_file_coverage(
            {}, ["backend/internal/x/types.go"], Decimal("85"), {}, self.root
        )
        self.assertTrue(passed)
        self.assertEqual(reached, [])
        self.assertEqual(len(results), 1)
        self.assertEqual(results[0][1], "no statements")
        report = format_file_report(results, Decimal("85"), reached)
        self.assertIn("no statements", report)
        self.assertNotIn("Failing files", report)

    def test_file_gate_missing_file_with_func_fails_at_zero(self) -> None:
        (self.root / "backend" / "internal" / "x").mkdir(parents=True)
        (self.root / "backend" / "internal" / "x" / "work.go").write_text(
            "package x\n\nfunc Work() int { return 1 }\n", encoding="utf-8"
        )
        results, reached, passed = evaluate_file_coverage(
            {}, ["backend/internal/x/work.go"], Decimal("85"), {}, self.root
        )
        self.assertFalse(passed)
        self.assertEqual(reached, [])
        self.assertEqual(len(results), 1)
        self.assertEqual(results[0][1], "FAIL")
        report = format_file_report(results, Decimal("85"), reached)
        self.assertIn("0.0%", report)
        self.assertIn("Failing files: internal/x/work.go", report)

    def test_changed_go_files_ignores_test_generated_and_excluded(self) -> None:
        repo = self.root / "repo"
        repo.mkdir()
        self._git(repo, "init", "-q")
        (repo / "backend" / "internal" / "keep").mkdir(parents=True)
        (repo / "backend" / "internal" / "keep" / "a.go").write_text("package keep\n", encoding="utf-8")
        self._git(repo, "add", ".")
        self._git(repo, "commit", "-q", "-m", "base")
        base = self._git(repo, "rev-parse", "HEAD")
        (repo / "backend" / "internal" / "keep" / "a.go").write_text("package keep // changed\n", encoding="utf-8")
        (repo / "backend" / "internal" / "keep" / "a_test.go").write_text("package keep\n", encoding="utf-8")
        (repo / "backend" / "internal" / "keep" / "gen").mkdir()
        (repo / "backend" / "internal" / "keep" / "gen" / "x.go").write_text("package gen\n", encoding="utf-8")
        (repo / "backend" / "internal" / "keep" / "y.gen.go").write_text("package keep\n", encoding="utf-8")
        (repo / "backend" / "cmd" / "cli").mkdir(parents=True)
        (repo / "backend" / "cmd" / "cli" / "main.go").write_text("package main\n", encoding="utf-8")
        (repo / "README.md").write_text("x\n", encoding="utf-8")
        self._git(repo, "add", ".")
        self._git(repo, "commit", "-q", "-m", "change")
        changed = changed_go_files(base, [("cmd/*", "command entry points")], repo)
        self.assertEqual(changed, ["backend/internal/keep/a.go"])
        self.assertTrue(is_generated_file("backend/internal/x/gen/y.go"))
        self.assertTrue(is_generated_file("backend/internal/x/y.gen.go"))
        self.assertFalse(is_generated_file("backend/internal/x/y.go"))
        self.assertTrue(is_excluded_file("backend/cmd/cli/main.go", [("cmd/*", "command entry points")]))

    def test_baseline_parse_format_and_candidates(self) -> None:
        baseline_file = self.root / "baseline.txt"
        baseline_file.write_text("# comment\ninternal/a/a.go 42.5\n\ninternal/b/b.go 0.0\n", encoding="utf-8")
        baseline = parse_baseline(baseline_file)
        self.assertEqual(baseline, {"internal/a/a.go": Decimal("42.5"), "internal/b/b.go": Decimal("0.0")})
        self.assertEqual(format_baseline(baseline), "internal/a/a.go 42.5\ninternal/b/b.go 0.0\n")
        baseline_file.write_text("internal/a/a.go notanumber\n", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "invalid baseline percent"):
            parse_baseline(baseline_file)
        baseline_file.write_text("internal/a/a.go 1.0\ninternal/a/a.go 2.0\n", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "duplicate baseline"):
            parse_baseline(baseline_file)

        files = parse_file_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/internal/low/a.go:1.1,1.2 2 1\n"
            "github.com/jooservices/xflickr/internal/low/a.go:2.1,2.2 1 0\n"
            "github.com/jooservices/xflickr/internal/high/a.go:1.1,1.2 10 1\n"
            "github.com/jooservices/xflickr/internal/gen/x.go:1.1,1.2 1 0\n",
        )
        candidates = baseline_candidates(files, Decimal("85"), [("internal/gen", "generated")])
        self.assertEqual(candidates, {"internal/low/a.go": Decimal("66.6")})
        self.assertNotIn("internal/high/a.go", candidates)
        self.assertNotIn("internal/gen/x.go", candidates)

    def test_baseline_floor_equal_lower_and_reached(self) -> None:
        files = parse_file_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/internal/base/a.go:1.1,1.2 5 1\n"
            "github.com/jooservices/xflickr/internal/base/a.go:2.1,2.2 5 0\n"
            "github.com/jooservices/xflickr/internal/reached/a.go:1.1,1.2 10 1\n",
        )
        baseline = {"internal/base/a.go": Decimal("50.0"), "internal/reached/a.go": Decimal("10.0")}
        results, reached, passed = evaluate_file_coverage(
            files,
            ["backend/internal/base/a.go", "backend/internal/reached/a.go"],
            Decimal("85"),
            baseline,
            self.root,
        )
        self.assertTrue(passed)
        self.assertEqual(reached, ["internal/reached/a.go"])
        statuses = {coverage.file: status for coverage, status in results}
        self.assertEqual(statuses["internal/base/a.go"], "BASELINE")
        self.assertEqual(statuses["internal/reached/a.go"], "PASS")
        self.assertIn("delete its baseline line", format_file_report(results, Decimal("85"), reached))

        lower = dict(baseline)
        lower["internal/base/a.go"] = Decimal("50.1")
        _results, _reached, passed_lower = evaluate_file_coverage(
            files, ["backend/internal/base/a.go"], Decimal("85"), lower, self.root
        )
        self.assertFalse(passed_lower)
        self.assertEqual(baseline_reached(files, baseline, Decimal("85")), ["internal/reached/a.go"])

    def test_unchanged_baseline_drop_fails(self) -> None:
        files = parse_file_profile_from_text(
            self.root / "cover.out",
            "github.com/jooservices/xflickr/internal/base/a.go:1.1,1.2 1 0\n",
        )
        results, _reached, passed = evaluate_file_coverage(
            files, [], Decimal("85"), {"internal/base/a.go": Decimal("10.0")}, self.root
        )
        self.assertFalse(passed)
        self.assertEqual(results[0][1], "FAIL")

    def test_main_write_baseline_and_file_gate(self) -> None:
        profile = self.root / "cover.out"
        profile.write_text(
            "mode: set\n"
            "github.com/jooservices/xflickr/internal/low/a.go:1.1,1.2 1 0\n"
            "github.com/jooservices/xflickr/internal/high/a.go:1.1,1.2 10 1\n",
            encoding="utf-8",
        )
        exclude = self.root / "exclude.txt"
        exclude.write_text("cmd/* | entry points\n", encoding="utf-8")
        baseline_file = self.root / "baseline.txt"
        self.assertEqual(
            main([str(profile), "--exclude", str(exclude), "--write-baseline", str(baseline_file)]), 0
        )
        self.assertEqual(baseline_file.read_text(encoding="utf-8"), "internal/low/a.go 0.0\n")
        baseline_file.write_text("", encoding="utf-8")

        with mock.patch("coverage_gate.changed_go_files", return_value=["backend/internal/low/a.go"]), mock.patch(
            "coverage_gate.repo_root", return_value=self.root
        ):
            code = main(
                [
                    str(profile),
                    "--exclude",
                    str(exclude),
                    "--check-files",
                    "--files-only",
                    "--base",
                    "origin/develop",
                    "--baseline",
                    str(baseline_file),
                ]
            )
        self.assertEqual(code, 1)

    def _git(self, repo: Path, *args: str) -> str:
        completed = subprocess.run(
            ["git", "-c", "user.email=test@example.com", "-c", "user.name=Test", *args],
            cwd=repo,
            capture_output=True,
            text=True,
            check=True,
        )
        return completed.stdout.strip()


def parse_profile_from_text(path: Path, entries: str):
    path.write_text("mode: set\n" + entries, encoding="utf-8")
    return parse_profile(path)


def parse_file_profile_from_text(path: Path, entries: str):
    path.write_text("mode: set\n" + entries, encoding="utf-8")
    return parse_file_profile(path)


if __name__ == "__main__":
    unittest.main()
