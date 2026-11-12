#!/usr/bin/env python3
"""Tests for go_function_gate.py: growth inside an old function fails the gate."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
GATE = SCRIPT_DIR / "go_function_gate.py"
FILE = "pkg/fn.go"
FUNC = "Work"


def func_source(name: str, ifs: int = 0, params: int = 1, lines: int = 0) -> str:
    """Return a Go function with 1 + ifs complexity and 4 + 3 * ifs + lines lines."""
    signature = ", ".join(f"p{i} int" for i in range(params))
    body = ["\tx := 0"]
    body += [f"\tif x < {i} {{\n\t\tx++\n\t}}" for i in range(ifs)]
    body += ["\tx++"] * lines
    body.append("\treturn x")
    return f"func {name}({signature}) int {{\n" + "\n".join(body) + "\n}\n"


def go_file(*functions: str) -> str:
    return "package pkg\n\n" + "\n".join(functions)


class FunctionGateTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Path(tmp.name)
        self.git("init", "-q")
        self.git("config", "user.email", "test@example.com")
        self.git("config", "user.name", "Test")

    def git(self, *args: str):
        subprocess.run(["git", *args], cwd=self.repo, check=True, capture_output=True, text=True)

    def write(self, source: str):
        path = self.repo / FILE
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(source, encoding="utf-8")

    def commit(self, source: str):
        self.write(source)
        self.git("add", "-A")
        self.git("commit", "-q", "-m", "base")

    def run_gate(self):
        return subprocess.run(
            [sys.executable, "-I", str(GATE), "--base", "HEAD"],
            cwd=self.repo,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_fails_and_names_old_function_above_limit_that_grows(self):
        self.commit(go_file(func_source(FUNC, ifs=11)))
        self.write(go_file(func_source(FUNC, ifs=12)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(FUNC, result.stderr)

    def test_passes_unchanged_old_function_when_file_changes_elsewhere(self):
        self.commit(go_file(func_source(FUNC, ifs=11), func_source("Other")))
        self.write(go_file(func_source(FUNC, ifs=11), func_source("Other", ifs=1)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_passes_function_below_limit_that_grows_to_limit(self):
        self.commit(go_file(func_source(FUNC, ifs=7)))
        self.write(go_file(func_source(FUNC, ifs=9)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_fails_function_below_limit_that_grows_past_limit(self):
        self.commit(go_file(func_source(FUNC, ifs=7)))
        self.write(go_file(func_source(FUNC, ifs=10)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(FUNC, result.stderr)

    def test_passes_old_function_above_limit_that_shrinks(self):
        self.commit(go_file(func_source(FUNC, ifs=11)))
        self.write(go_file(func_source(FUNC, ifs=10)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_fails_new_function_above_limit(self):
        self.commit("package pkg\n")
        self.write(go_file(func_source(FUNC, ifs=11)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(FUNC, result.stderr)

    def test_passes_new_function_within_limits(self):
        self.commit("package pkg\n")
        self.write(go_file(func_source(FUNC, ifs=9, params=4)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_fails_old_function_above_length_limit_that_grows(self):
        self.commit(go_file(func_source(FUNC, lines=36)))
        self.write(go_file(func_source(FUNC, lines=37)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(FUNC, result.stderr)

    def test_fails_function_at_parameter_limit_that_gains_a_parameter(self):
        self.commit(go_file(func_source(FUNC, params=4)))
        self.write(go_file(func_source(FUNC, params=5)))

        result = self.run_gate()

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(FUNC, result.stderr)


if __name__ == "__main__":
    unittest.main()
