#!/usr/bin/env python3
"""Partition Go unit tests and integration tests across module shards."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BACKEND = ROOT / "backend"
MODULE_PREFIX = "github.com/jooservices/xflickr/"
INTEGRATION_MARKERS = ("XFLICKR_INTEGRATION_TEST", "XFLICKR_DBTEST_INTEGRATION_TEST")
GO_TEST_GROUPS = {
    "core": ("accounts", "fakeflickr", "identity", "migrations", "platform", "settings", "cmd", "pkg"),
    "catalog": ("catalog", "crawl", "quota", "search", "sharing"),
    "media": ("ai", "home", "media", "sync"),
    "operations": ("cluster", "ops", "schedule"),
}
INTEGRATION_MODULE_SHARDS = {
    "cluster": ("cluster",),
    "catalog": ("catalog",),
    "platform": ("platform",),
    "schedule": ("schedule",),
    "runtime": ("accounts", "ai", "cmd", "home", "identity", "media", "ops", "settings"),
    "services": ("crawl", "quota", "sharing", "sync"),
    "search": ("search",),
}


def module_name(package: str) -> str:
    """Return the first internal module or public package for an import path."""
    relative = package.removeprefix(MODULE_PREFIX)
    parts = relative.split("/")
    if parts[0] == "internal" and len(parts) > 1:
        return parts[1]
    if parts[0] in {"cmd", "migrations", "pkg"}:
        return parts[0]
    raise ValueError(f"no Go test group for package: {package}")


def group_packages(packages: list[str] | tuple[str, ...]) -> dict[str, list[str]]:
    """Assign every Go package to exactly one unit-test group."""
    by_module = {module: group for group, modules in GO_TEST_GROUPS.items() for module in modules}
    groups = {group: [] for group in GO_TEST_GROUPS}
    for package in sorted(set(packages)):
        module = module_name(package)
        group = by_module.get(module)
        if group is None:
            raise ValueError(f"no Go test group for package {package}")
        groups[group].append(package)
    return groups


def integration_modules(package_tests: dict[str, str]) -> dict[str, list[str]]:
    """Keep only packages whose tests opt into integration behavior."""
    modules: dict[str, list[str]] = {}
    for package, source in package_tests.items():
        if any(marker in source for marker in INTEGRATION_MARKERS):
            modules.setdefault(module_name(package), []).append(package)
    return {module: sorted(packages) for module, packages in sorted(modules.items())}


def integration_test_packages(root: Path = BACKEND) -> dict[str, list[str]]:
    """Find test packages that opt into PostgreSQL or service integration."""
    package_tests: dict[str, str] = {}
    for path in sorted(root.rglob("*_test.go")):
        source = path.read_text(encoding="utf-8")
        if any(marker in source for marker in INTEGRATION_MARKERS):
            relative = path.parent.relative_to(root).as_posix()
            package = MODULE_PREFIX + relative
            package_tests[package] = package_tests.get(package, "") + source
    return integration_modules(package_tests)


def integration_test_names(root: Path = BACKEND) -> dict[str, list[str]]:
    """Find Go tests declared in files that opt into integration behavior."""
    tests: dict[str, set[str]] = {}
    for path in sorted(root.rglob("*_test.go")):
        source = path.read_text(encoding="utf-8")
        if not any(marker in source for marker in INTEGRATION_MARKERS):
            continue
        relative = path.parent.relative_to(root).as_posix()
        module = module_name(MODULE_PREFIX + relative)
        tests.setdefault(module, set()).update(
            re.findall(r"^func\s+(Test[A-Za-z0-9_]*)\s*\(", source, re.MULTILINE)
        )
    return {module: sorted(names) for module, names in sorted(tests.items())}


def integration_test_pattern(module: str, tests: dict[str, list[str]]) -> str:
    """Build a go test -run pattern for one module's integration files."""
    names = tests.get(module, [])
    if not names:
        raise ValueError(f"integration module {module} has no test functions in marked files")
    return "^(" + "|".join(names) + ")$"


def integration_module_shards(modules: list[str]) -> list[dict[str, str | bool]]:
    """Group every discovered integration module into a balanced job shard."""
    selected = set(modules)
    assigned = {module for shard_modules in INTEGRATION_MODULE_SHARDS.values() for module in shard_modules}
    unknown = selected - assigned
    if unknown:
        raise ValueError("integration modules missing shard assignment: " + ", ".join(sorted(unknown)))
    result = []
    for name, shard_modules in INTEGRATION_MODULE_SHARDS.items():
        assigned = [module for module in shard_modules if module in selected]
        if assigned:
            result.append({
                "name": name,
                "modules": ",".join(assigned),
                "elasticsearch": "search" in assigned,
            })
    return result


def group_names() -> list[str]:
    """Return the configured package group names in stable order."""
    return sorted(GO_TEST_GROUPS)


def go_packages() -> list[dict[str, object]]:
    runner = ROOT / "docker/scripts/run.sh"
    result = subprocess.run(
        [str(runner), "go", "go", "list", "-json", "./..."],
        cwd=BACKEND,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError("go list failed: " + result.stderr.strip())
    decoder = json.JSONDecoder()
    packages = []
    remainder = result.stdout.lstrip()
    while remainder:
        item, offset = decoder.raw_decode(remainder)
        packages.append(item)
        remainder = remainder[offset:].lstrip()
    return packages


def print_packages(packages: list[str]) -> None:
    if not packages:
        raise ValueError("the requested test shard has no packages")
    print("\n".join(packages))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("go-groups")
    subparsers.add_parser("integration-modules")
    subparsers.add_parser("integration-shards")
    package_parser = subparsers.add_parser("go-packages")
    package_parser.add_argument("group", choices=GO_TEST_GROUPS)
    module_parser = subparsers.add_parser("integration-packages")
    module_parser.add_argument("module")
    test_parser = subparsers.add_parser("integration-test-pattern")
    test_parser.add_argument("module")
    args = parser.parse_args()
    try:
        if args.command == "go-groups":
            print(json.dumps(group_names()))
        elif args.command == "integration-modules":
            print(json.dumps(list(integration_test_packages())))
        elif args.command == "integration-shards":
            print(json.dumps(integration_module_shards(list(integration_test_packages()))))
        elif args.command == "integration-packages":
            modules = integration_test_packages()
            print_packages(modules.get(args.module, []))
        elif args.command == "integration-test-pattern":
            print(integration_test_pattern(args.module, integration_test_names()))
        else:
            packages = go_packages()
            imports = [str(package["ImportPath"]) for package in packages]
            print_packages(group_packages(imports)[args.group])
    except (OSError, ValueError, RuntimeError, json.JSONDecodeError) as error:
        print(f"test shard discovery failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
