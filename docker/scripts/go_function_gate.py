#!/usr/bin/env python3
"""Fail when a Go function above cyclomatic complexity 10 grows (JOOXF-511, JOOXF-516).

Every function of the changed Go files (not test files, as in .golangci.yml) is
compared with the same function at the merge base with --base. A function above
the limit that grows fails; a function below it may grow up to the limit; a new
function must be within it. Length and parameter count are review signals, not
gates, so they are not measured. Usage: go_function_gate.py [--base REF]
"""

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path

COMPLEXITY_LIMIT = 10
STATS_SOURCE = Path(__file__).resolve().parents[2] / "backend" / "cmd" / "gofuncstats" / "main.go"


class GateError(Exception):
    """A tool failure (git or go), not a rule violation."""


def run(command, root: Path, stdin: str = "") -> str:
    result = subprocess.run(command, cwd=root, input=stdin, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise GateError(f"{' '.join(command[:2])} failed: {result.stderr.strip()}")
    return result.stdout


def changed_go_files(root: Path, base: str):
    """Return (path at base, path now) for each changed non-test Go file."""
    pairs = []
    for line in run(["git", "diff", "--name-status", "-M", base, "--", "*.go"], root).splitlines():
        fields = line.split("\t")
        if fields[0].startswith("R"):
            pairs.append((fields[1], fields[2]))
        elif fields[0][0] in "ACM":
            pairs.append((fields[1], fields[1]))
    untracked = run(["git", "ls-files", "--others", "--exclude-standard", "--", "*.go"], root)
    pairs += [(path, path) for path in untracked.splitlines()]
    return [(old, new) for old, new in pairs if new.endswith(".go") and not new.endswith("_test.go")]


def stats(root: Path, paths):
    """Return {path: {function name: complexity}} measured under root by gofuncstats."""
    if not paths:
        return {}
    output = run(["go", "run", str(STATS_SOURCE)], root, stdin="\n".join(paths) + "\n")
    result = {}
    for line in output.splitlines():
        path, name, complexity = line.split("\t")
        result.setdefault(path, {})[name] = int(complexity)
    return result


def base_stats(root: Path, merge_base: str, paths, tmp: Path):
    """Write the files as they were at the merge base under tmp and measure them."""
    base_files = set(run(["git", "ls-tree", "-r", "--name-only", merge_base], root).splitlines())
    present = [path for path in paths if path in base_files]
    for path in present:
        target = tmp / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(run(["git", "show", f"{merge_base}:{path}"], root), encoding="utf-8")
    return stats(tmp, present)


def violations(pairs, before, after):
    problems = []
    for old, new in pairs:
        old_functions = before.get(old, {})
        for name, now in after.get(new, {}).items():
            if now <= COMPLEXITY_LIMIT:
                continue
            was = old_functions.get(name)
            if was is None:
                problems.append(f"{new}: {name} is new with complexity {now} (limit {COMPLEXITY_LIMIT})")
            elif now > was:
                problems.append(
                    f"{new}: {name} grew in complexity {was} -> {now} (limit {COMPLEXITY_LIMIT})"
                )
    return problems


def check(base: str):
    root = Path(run(["git", "rev-parse", "--show-toplevel"], Path.cwd()).strip())
    merge_base = run(["git", "merge-base", base, "HEAD"], root).strip()
    pairs = changed_go_files(root, merge_base)
    if not pairs:
        return []
    with tempfile.TemporaryDirectory() as tmp:
        before = base_stats(root, merge_base, [old for old, _ in pairs], Path(tmp))
    after = stats(root, [new for _, new in pairs])
    return violations(pairs, before, after)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--base", default="origin/develop", help="ref to compare with (default: origin/develop)")
    args = parser.parse_args(argv)
    try:
        problems = check(args.base)
    except GateError as err:
        print(f"Go function gate: {err}", file=sys.stderr)
        return 2
    for problem in problems:
        print(problem, file=sys.stderr)
    if problems:
        print(f"Go function gate: FAILED ({len(problems)} finding(s))", file=sys.stderr)
        return 1
    print("Go function gate: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
