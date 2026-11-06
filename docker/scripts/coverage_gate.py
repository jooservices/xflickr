#!/usr/bin/env python3
"""Calculate per-package Go statement coverage from a coverprofile."""

from __future__ import annotations

import argparse
import fnmatch
import os
import posixpath
import subprocess
import sys
from dataclasses import dataclass
from decimal import ROUND_DOWN, Decimal, InvalidOperation
from pathlib import Path

MODULE_PREFIX = "github.com/jooservices/xflickr/"


@dataclass(frozen=True)
class PackageCoverage:
    package: str
    covered: int
    total: int

    @property
    def ratio(self) -> Decimal:
        return Decimal(self.covered) / Decimal(self.total)


@dataclass(frozen=True)
class FileCoverage:
    file: str
    covered: int
    total: int

    @property
    def ratio(self) -> Decimal:
        return Decimal(self.covered) / Decimal(self.total)


def _iter_blocks(path: Path):
    mode_seen = False
    for line_number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw_line.strip()
        if not line:
            continue
        if line.startswith("mode: "):
            if mode_seen or line.split(maxsplit=1)[1] not in {"set", "count", "atomic"}:
                raise ValueError(f"invalid coverage mode at line {line_number}")
            mode_seen = True
            continue
        if not mode_seen:
            raise ValueError("coverage profile must begin with a mode header")
        fields = line.split()
        if len(fields) != 3:
            raise ValueError(f"malformed coverage profile line {line_number}")
        location, statements_text, count_text = fields
        file_path, separator, position = location.partition(":")
        if not separator or not file_path.startswith(MODULE_PREFIX):
            raise ValueError(f"unsupported coverage path at line {line_number}: {file_path}")
        try:
            statements, count = int(statements_text), int(count_text)
        except ValueError as error:
            raise ValueError(f"invalid statement count at line {line_number}") from error
        if statements < 0 or count < 0:
            raise ValueError(f"invalid coverage values at line {line_number}")
        if statements == 0:
            continue
        relative = file_path[len(MODULE_PREFIX) :]
        yield relative, (relative, position), statements, count > 0


def _merged_blocks(path: Path) -> dict[tuple[str, str], list]:
    blocks: dict[tuple[str, str], list] = {}
    for relative, key, statements, covered in _iter_blocks(path):
        existing = blocks.get(key)
        if existing is None:
            blocks[key] = [relative, statements, covered]
        else:
            existing[1] = max(existing[1], statements)
            existing[2] = existing[2] or covered
    if not blocks:
        raise ValueError("coverage profile contains no executable statements")
    return blocks


def parse_profile(path: Path) -> dict[str, PackageCoverage]:
    packages: dict[str, list[int]] = {}
    for relative, statements, covered in _merged_blocks(path).values():
        package = posixpath.dirname(relative)
        values = packages.setdefault(package, [0, 0])
        values[1] += statements
        if covered:
            values[0] += statements
    return {
        package: PackageCoverage(package, values[0], values[1])
        for package, values in packages.items()
    }


def parse_file_profile(path: Path) -> dict[str, FileCoverage]:
    files: dict[str, list[int]] = {}
    for relative, statements, covered in _merged_blocks(path).values():
        values = files.setdefault(relative, [0, 0])
        values[1] += statements
        if covered:
            values[0] += statements
    return {
        file: FileCoverage(file, values[0], values[1])
        for file, values in files.items()
    }


def is_generated_file(path: str) -> bool:
    return path.endswith(".gen.go") or "gen" in posixpath.dirname(path).split("/")


def profile_relative(path: str) -> str:
    return path[len("backend/") :] if path.startswith("backend/") else path


def is_excluded_file(path: str, exclusions: list[tuple[str, str]]) -> bool:
    relative = profile_relative(path)
    package = posixpath.dirname(relative)
    for pattern, _reason in exclusions:
        if fnmatch.fnmatchcase(package, pattern) or fnmatch.fnmatchcase(relative, pattern):
            return True
        if relative.startswith(pattern.rstrip("/") + "/"):
            return True
    return False


def changed_go_files(
    base: str,
    exclusions: list[tuple[str, str]] | tuple[tuple[str, str], ...] = (),
    root: Path | None = None,
) -> list[str]:
    if not base:
        raise ValueError("base ref must not be empty")
    completed = subprocess.run(
        ["git", "diff", "--name-only", f"{base}...HEAD"],
        cwd=root,
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        raise ValueError(f"git diff against {base} failed: {completed.stderr.strip()}")
    changed: list[str] = []
    for line in completed.stdout.splitlines():
        path = line.strip()
        if not path or not path.endswith(".go") or path.endswith("_test.go"):
            continue
        if is_generated_file(path) or is_excluded_file(path, list(exclusions)):
            continue
        changed.append(path)
    return sorted(changed)


def repo_root() -> Path:
    completed = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode == 0 and completed.stdout.strip():
        return Path(completed.stdout.strip())
    return Path.cwd()


def parse_exclusions(path: Path) -> list[tuple[str, str]]:
    exclusions: list[tuple[str, str]] = []
    seen: set[str] = set()
    for line_number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if "|" not in line:
            raise ValueError(f"exclusion line {line_number} must contain 'pattern | reason'")
        pattern, reason = (part.strip() for part in line.split("|", 1))
        if not pattern or not reason:
            raise ValueError(f"exclusion line {line_number} requires both a pattern and a reason")
        if pattern in seen:
            raise ValueError(f"duplicate exclusion pattern at line {line_number}: {pattern}")
        seen.add(pattern)
        exclusions.append((pattern, reason))
    return exclusions


def evaluate(
    packages: dict[str, PackageCoverage],
    exclusions: list[tuple[str, str]],
    threshold: Decimal,
) -> tuple[list[tuple[PackageCoverage, str]], list[tuple[str, str]], bool]:
    if threshold <= 0 or threshold > 100:
        raise ValueError("coverage threshold must be greater than 0 and at most 100")
    unmatched = {pattern for pattern, _reason in exclusions}
    measured: list[tuple[PackageCoverage, str]] = []
    excluded: list[tuple[str, str]] = []
    passed = True
    for package in packages.values():
        matched_reason = None
        for pattern, reason in exclusions:
            if fnmatch.fnmatchcase(package.package, pattern):
                matched_reason = reason
                unmatched.discard(pattern)
                break
        if matched_reason is not None:
            excluded.append((package.package, matched_reason))
            continue
        status = "PASS" if package.ratio * 100 >= threshold else "FAIL"
        if status == "FAIL":
            passed = False
        measured.append((package, status))
    if unmatched:
        raise ValueError("exclusion pattern matched no package(s): " + ", ".join(sorted(unmatched)))
    measured.sort(key=lambda item: (item[0].ratio, item[0].package))
    excluded.sort(key=lambda item: item[0])
    return measured, excluded, passed


def parse_baseline(path: Path) -> dict[str, Decimal]:
    baseline: dict[str, Decimal] = {}
    for line_number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        fields = line.split()
        if len(fields) != 2:
            raise ValueError(f"baseline line {line_number} must be 'path percent'")
        file, percent_text = fields
        try:
            percent = Decimal(percent_text)
        except InvalidOperation as error:
            raise ValueError(f"invalid baseline percent at line {line_number}") from error
        if percent < 0 or percent > 100:
            raise ValueError(f"baseline percent out of range at line {line_number}")
        if file in baseline:
            raise ValueError(f"duplicate baseline entry at line {line_number}: {file}")
        baseline[file] = percent
    return baseline


def format_baseline(baseline: dict[str, Decimal]) -> str:
    return "".join(f"{file} {baseline[file]:.1f}\n" for file in sorted(baseline))


def baseline_candidates(
    files: dict[str, FileCoverage],
    threshold: Decimal,
    exclusions: list[tuple[str, str]],
) -> dict[str, Decimal]:
    candidates: dict[str, Decimal] = {}
    for file, coverage in files.items():
        if coverage.ratio * 100 < threshold and not is_generated_file(file) and not is_excluded_file(file, exclusions):
            candidates[file] = (coverage.ratio * 100).quantize(Decimal("0.1"), rounding=ROUND_DOWN)
    return candidates


def baseline_reached(
    files: dict[str, FileCoverage],
    baseline: dict[str, Decimal],
    threshold: Decimal,
) -> list[str]:
    return sorted(
        file
        for file in baseline
        if file in files and files[file].ratio * 100 >= threshold
    )


def evaluate_file_coverage(
    files: dict[str, FileCoverage],
    changed: list[str],
    threshold: Decimal,
    baseline: dict[str, Decimal] | None = None,
    repo_root: Path | None = None,
) -> tuple[list[tuple[FileCoverage, str]], list[str], bool]:
    if threshold <= 0 or threshold > 100:
        raise ValueError("coverage threshold must be greater than 0 and at most 100")
    floors = baseline or {}
    results: list[tuple[FileCoverage, str]] = []
    passed = True
    seen: set[str] = set()
    for repo_path in changed:
        relative = profile_relative(repo_path)
        coverage = files.get(relative)
        if coverage is None:
            if repo_root is not None and not (repo_root / repo_path).exists():
                continue
            coverage = FileCoverage(relative, 0, 1)
        seen.add(relative)
        percent = coverage.ratio * 100
        if relative in floors and percent < floors[relative]:
            status = "FAIL"
        elif percent >= threshold:
            status = "PASS"
        elif relative in floors:
            status = "BASELINE"
        else:
            status = "FAIL"
        if status == "FAIL":
            passed = False
        results.append((coverage, status))
    for file in sorted(floors):
        if file in seen or file not in files:
            continue
        coverage = files[file]
        if coverage.ratio * 100 < floors[file]:
            passed = False
            results.append((coverage, "FAIL"))
    results.sort(key=lambda item: (item[0].ratio, item[0].file))
    return results, baseline_reached(files, floors, threshold), passed


def format_file_report(
    results: list[tuple[FileCoverage, str]],
    threshold: Decimal,
    reached: list[str] | tuple[str, ...] = (),
) -> str:
    lines = [f"Go file coverage (minimum {threshold:.1f}%, changed files):"]
    for coverage, status in results:
        percent = coverage.ratio * 100
        lines.append(f"{percent:5.1f}%  {status:8}  {coverage.file}")
    failing = [coverage.file for coverage, status in results if status == "FAIL"]
    if failing:
        lines.append("Failing files: " + ", ".join(failing))
    for file in reached:
        lines.append(f"Baseline reached {threshold:.1f}%: {file} (delete its baseline line)")
    lines.append(
        f"File coverage gate {'passed' if not failing else 'failed'}: "
        f"{len(results)} changed Go file(s) checked, {len(failing)} failing."
    )
    return "\n".join(lines)


def format_report(
    measured: list[tuple[PackageCoverage, str]],
    excluded: list[tuple[str, str]],
    threshold: Decimal,
) -> str:
    lines = [f"Go package coverage (minimum {threshold:.1f}%):"]
    for package, status in measured:
        percent = package.ratio * 100
        lines.append(f"{percent:5.1f}%  {status:4}  {package.package}")
    for package, reason in excluded:
        lines.append(f"  ---   SKIP  {package} ({reason})")
    lines.append(
        f"Coverage gate {'passed' if all(status == 'PASS' for _package, status in measured) else 'failed'}: "
        f"{len(measured)} measured package(s), {len(excluded)} excluded."
    )
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", type=Path)
    parser.add_argument("--exclude", required=True, type=Path, help="package exclusion allowlist")
    parser.add_argument(
        "--threshold",
        default=os.environ.get("COVERAGE_THRESHOLD", "85"),
        help="minimum statement coverage percentage (default: 85)",
    )
    parser.add_argument("--check-files", action="store_true", help="also run the per-file gate for changed files")
    parser.add_argument("--base", default="origin/develop", help="base ref for changed files (default: origin/develop)")
    parser.add_argument("--files-only", action="store_true", help="run only the per-file gate, not the package gate")
    parser.add_argument(
        "--baseline",
        type=Path,
        default=Path(__file__).parent / "coverage-file-baseline.txt",
        help="per-file baseline for pre-existing files below the threshold",
    )
    parser.add_argument("--write-baseline", type=Path, help="write the baseline for files below the threshold and exit")
    args = parser.parse_args(argv)
    try:
        threshold = Decimal(args.threshold)
        exclusions = parse_exclusions(args.exclude)
        if args.write_baseline is not None:
            files = parse_file_profile(args.profile)
            baseline = baseline_candidates(files, threshold, exclusions)
            args.write_baseline.write_text(format_baseline(baseline), encoding="utf-8")
            print(f"Wrote {len(baseline)} baseline entr(ies) to {args.write_baseline}")
            return 0
        passed = True
        if args.files_only:
            if not args.check_files:
                raise ValueError("--files-only requires --check-files")
        else:
            packages = parse_profile(args.profile)
            measured, excluded, package_passed = evaluate(packages, exclusions, threshold)
            print(format_report(measured, excluded, threshold))
            passed = passed and package_passed
        if args.check_files:
            files = parse_file_profile(args.profile)
            changed = changed_go_files(args.base, exclusions)
            baseline = parse_baseline(args.baseline) if args.baseline.exists() else {}
            results, reached, file_passed = evaluate_file_coverage(
                files, changed, threshold, baseline, repo_root()
            )
            print(format_file_report(results, threshold, reached))
            passed = passed and file_passed
    except (InvalidOperation, OSError, ValueError) as error:
        print(f"coverage gate: {error}", file=sys.stderr)
        return 2
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
