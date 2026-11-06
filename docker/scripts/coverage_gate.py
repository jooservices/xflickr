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
from decimal import Decimal, InvalidOperation
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
    args = parser.parse_args(argv)
    try:
        threshold = Decimal(args.threshold)
        packages = parse_profile(args.profile)
        exclusions = parse_exclusions(args.exclude)
        measured, excluded, passed = evaluate(packages, exclusions, threshold)
    except (InvalidOperation, OSError, ValueError) as error:
        print(f"coverage gate: {error}", file=sys.stderr)
        return 2
    print(format_report(measured, excluded, threshold))
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
