#!/usr/bin/env python3
"""Tests for module_cycles.py: a dependency cycle between business modules fails."""

import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
CHECK = SCRIPT_DIR / "module_cycles.py"
REPO_INTERNAL = SCRIPT_DIR.parents[1] / "backend" / "internal"
IMPORT_ROOT = "example.com/m/internal"


def run_check(internal: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, "-I", str(CHECK), str(internal)],
        capture_output=True,
        text=True,
        check=False,
    )


class ModuleCyclesTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.internal = Path(tmp.name) / "internal"

    def write(self, rel: str, body: str):
        path = self.internal / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(textwrap.dedent(body), encoding="utf-8")

    def test_fails_and_names_cycle_between_two_modules(self):
        self.write("a/a.go", f'package a\n\nimport "{IMPORT_ROOT}/b"\n')
        self.write("b/b.go", f'package b\n\nimport "{IMPORT_ROOT}/a"\n')

        result = run_check(self.internal)

        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("a -> b -> a", result.stderr)

    def test_passes_tree_without_cycle(self):
        self.write("a/a.go", f'package a\n\nimport "{IMPORT_ROOT}/b"\n')
        self.write("b/b.go", "package b\n")

        result = run_check(self.internal)

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_finds_cycle_through_import_block_with_alias(self):
        self.write("a/a.go", f'package a\n\nimport (\n\t"fmt"\n\tx "{IMPORT_ROOT}/b"\n)\n')
        self.write("b/b.go", f'package b\n\nimport (\n\t"{IMPORT_ROOT}/c"\n)\n')
        self.write("c/c.go", f'package c\n\nimport "{IMPORT_ROOT}/a"\n')

        result = run_check(self.internal)

        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("a -> b -> c -> a", result.stderr)

    def test_ignores_test_files_self_imports_platform_and_fakeflickr(self):
        self.write("a/a_test.go", f'package a\n\nimport "{IMPORT_ROOT}/b"\n')
        self.write("a/app/app.go", f'package app\n\nimport "{IMPORT_ROOT}/a/domain"\n')
        self.write("a/domain/d.go", "package domain\n")
        self.write("a/a.go", f'package a\n\nimport "{IMPORT_ROOT}/platform/x"\n')
        self.write("b/b.go", f'package b\n\nimport "{IMPORT_ROOT}/a/domain"\n')
        self.write("platform/x/x.go", f'package x\n\nimport "{IMPORT_ROOT}/a"\n')
        self.write("fakeflickr/f.go", f'package f\n\nimport "{IMPORT_ROOT}/b"\n')
        self.write("b/fake.go", f'package b\n\nimport "{IMPORT_ROOT}/fakeflickr"\n')

        result = run_check(self.internal)

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_fails_early_when_internal_directory_is_missing(self):
        result = run_check(self.internal / "missing")

        self.assertEqual(result.returncode, 2)

    def test_real_repository_has_no_module_cycle(self):
        result = run_check(REPO_INTERNAL)

        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
