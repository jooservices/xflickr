#!/usr/bin/env python3
"""Fail when business modules under backend/internal depend on each other in a cycle.

A module is the first folder under internal/. Platform and fakeflickr are not
business modules, and only edges between business modules count (ADR 0008:
dependencies between modules are acyclic). Non-test Go imports only.

Usage: module_cycles.py [internal_dir]   (exit 0 acyclic, 1 cycle, 2 usage error)
"""

import re
import sys
from pathlib import Path

DEFAULT_INTERNAL = Path(__file__).resolve().parents[2] / "backend" / "internal"
EXCLUDED_MODULES = {"platform", "fakeflickr"}
IMPORT_BLOCK = re.compile(r"^import\s*\((.*?)^\)", re.MULTILINE | re.DOTALL)
IMPORT_SINGLE = re.compile(r'^import\s+(?:[\w.]+\s+)?"([^"]+)"', re.MULTILINE)
IMPORT_IN_BLOCK = re.compile(r'^\s*(?:[\w.]+\s+)?"([^"]+)"', re.MULTILINE)
INTERNAL_MARKER = "/internal/"


def file_imports(path: Path) -> list[str]:
    text = path.read_text(encoding="utf-8")
    imports = IMPORT_SINGLE.findall(text)
    for block in IMPORT_BLOCK.findall(text):
        imports.extend(IMPORT_IN_BLOCK.findall(block))
    return imports


def target_module(import_path: str) -> str | None:
    index = import_path.find(INTERNAL_MARKER)
    if index < 0:
        return None
    return import_path[index + len(INTERNAL_MARKER):].split("/")[0]


def build_graph(internal: Path) -> dict[str, set[str]]:
    modules = {
        child.name
        for child in internal.iterdir()
        if child.is_dir() and child.name not in EXCLUDED_MODULES
    }
    graph: dict[str, set[str]] = {module: set() for module in modules}
    for path in internal.rglob("*.go"):
        parts = path.relative_to(internal).parts
        if path.name.endswith("_test.go") or len(parts) < 2 or parts[0] not in graph:
            continue
        for import_path in file_imports(path):
            target = target_module(import_path)
            if target in graph and target != parts[0]:
                graph[parts[0]].add(target)
    return graph


def find_cycle(graph: dict[str, set[str]]) -> list[str] | None:
    done: set[str] = set()
    for start in sorted(graph):
        cycle = _search(start, graph, done, [])
        if cycle:
            return cycle
    return None


def _search(node: str, graph: dict[str, set[str]], done: set[str], stack: list[str]):
    if node in stack:
        return stack[stack.index(node):] + [node]
    if node in done:
        return None
    stack.append(node)
    for successor in sorted(graph[node]):
        cycle = _search(successor, graph, done, stack)
        if cycle:
            return cycle
    stack.pop()
    done.add(node)
    return None


def main(args: list[str]) -> int:
    internal = Path(args[0]) if args else DEFAULT_INTERNAL
    if not internal.is_dir():
        print(f"module_cycles: not a directory: {internal}", file=sys.stderr)
        return 2
    cycle = find_cycle(build_graph(internal))
    if cycle:
        print("module dependency cycle: " + " -> ".join(cycle), file=sys.stderr)
        return 1
    print("module dependencies are acyclic")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
