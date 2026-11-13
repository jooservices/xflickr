#!/usr/bin/env python3
"""Read shared tool pins and warn when local runtimes do not match."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SUPPORTED = {"go", "nodejs", "golangci-lint", "python"}
ALIASES = {"nodejs": "node"}


def parse_versions(text: str) -> dict[str, str]:
    """Parse the supported asdf-style tool entries, rejecting duplicates."""
    versions: dict[str, str] = {}
    for line_number, raw_line in enumerate(text.splitlines(), start=1):
        line = raw_line.split("#", 1)[0].strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) != 2 or parts[0] not in SUPPORTED:
            raise ValueError(f"unsupported .tool-versions entry at line {line_number}")
        name, version = parts
        key = ALIASES.get(name, name)
        if key in versions:
            raise ValueError(f"duplicate tool version at line {line_number}: {name}")
        if re.fullmatch(r"\d+(?:\.\d+){0,2}", version) is None:
            raise ValueError(f"invalid {name} version at line {line_number}")
        versions[key] = version
    required = {"go", "node", "golangci-lint", "python"}
    missing = sorted(required - versions.keys())
    if missing:
        raise ValueError(".tool-versions is missing: " + ", ".join(missing))
    return versions


def normalized_version(tool: str, value: str) -> str:
    """Extract the numeric version from a runtime's version output."""
    pattern = r"\d+(?:\.\d+){0,2}"
    if tool == "go":
        match = re.search(r"go(" + pattern + r")", value)
    else:
        match = re.search(pattern, value.lstrip("v"))
    if match is None:
        raise ValueError(f"could not read {tool} version from {value!r}")
    return match[1] if tool == "go" else match[0]


def version_matches(pin: str, installed: str) -> bool:
    """Compare all components in a pin, allowing asdf partial versions."""
    pinned_parts = pin.split(".")
    installed_parts = installed.split(".")
    return installed_parts[: len(pinned_parts)] == pinned_parts


def mismatches(pins: dict[str, str], installed: dict[str, str]) -> list[str]:
    """Describe local runtime differences using both pinned and actual values."""
    labels = {"go": "Go", "node": "Node", "python": "Python"}
    warnings = []
    for tool, label in labels.items():
        current = installed.get(tool)
        if current is None or not version_matches(pins[tool], normalized_version(tool, current)):
            value = current if current is not None else "not installed"
            warnings.append(f"{label} version differs: pinned {pins[tool]}, machine {value}")
    return warnings


def runtime_versions() -> dict[str, str]:
    commands = {"go": ["go", "version"], "node": ["node", "--version"], "python": [sys.executable, "--version"]}
    result: dict[str, str] = {}
    for tool, command in commands.items():
        try:
            output = subprocess.run(command, check=False, capture_output=True, text=True)
        except OSError:
            continue
        if output.returncode == 0:
            result[tool] = output.stdout.strip() or output.stderr.strip()
    return result


def write_github_output(path: Path, versions: dict[str, str]) -> None:
    """Expose pins as workflow outputs for setup actions."""
    values = [f"{key}={value}" for key, value in versions.items()]
    with path.open("a", encoding="utf-8") as output:
        output.write("\n".join(values) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--github-output", type=Path)
    args = parser.parse_args()
    try:
        versions = parse_versions((ROOT / ".tool-versions").read_text(encoding="utf-8"))
        if args.github_output:
            write_github_output(args.github_output, versions)
            print("Tool pins loaded from .tool-versions")
            return 0
        warnings = mismatches(versions, runtime_versions())
    except (OSError, ValueError) as error:
        print(f"Tool version check failed: {error}", file=sys.stderr)
        return 1
    for warning in warnings:
        print(f"WARNING: {warning}", file=sys.stderr)
    print(f"Tool version check complete: {len(warnings)} runtime difference(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
