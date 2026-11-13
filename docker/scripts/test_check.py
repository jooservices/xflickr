#!/usr/bin/env python3
"""Tests that make check preserves the local E2E gate."""

from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CHECK = ROOT / "docker/scripts/check.sh"


class CheckScriptTest(unittest.TestCase):
    def run_check(self, temporary: Path, verify_status: int = 0, e2e_status: int = 0) -> tuple[subprocess.CompletedProcess[str], list[str]]:
        bin_dir = temporary / "bin"
        bin_dir.mkdir()
        log = temporary / "make.log"
        fake_make = bin_dir / "make"
        fake_make.write_text(
            "#!/bin/sh\n"
            "for argument do target=$argument; done\n"
            "printf '%s\\n' \"$target\" >> \"$CHECK_LOG\"\n"
            "case \"$target\" in\n"
            "  verify) exit \"$VERIFY_STATUS\" ;;\n"
            "  e2e) exit \"$E2E_STATUS\" ;;\n"
            "  *) exit 2 ;;\n"
            "esac\n",
            encoding="utf-8",
        )
        fake_make.chmod(0o755)
        env = {
            **os.environ,
            "PATH": f"{bin_dir}{os.pathsep}{os.environ.get('PATH', '')}",
            "CHECK_LOG": str(log),
            "VERIFY_STATUS": str(verify_status),
            "E2E_STATUS": str(e2e_status),
        }
        result = subprocess.run([str(CHECK)], cwd=ROOT, env=env, capture_output=True, text=True, check=False)
        return result, log.read_text(encoding="utf-8").splitlines()

    def test_check_runs_verify_then_e2e(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result, targets = self.run_check(Path(directory))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(targets, ["verify", "e2e"])

    def test_check_propagates_e2e_failure(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result, targets = self.run_check(Path(directory), e2e_status=23)

        self.assertEqual(result.returncode, 23)
        self.assertEqual(targets, ["verify", "e2e"])

    def test_check_stops_when_verify_fails(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result, targets = self.run_check(Path(directory), verify_status=17)

        self.assertEqual(result.returncode, 17)
        self.assertEqual(targets, ["verify"])


if __name__ == "__main__":
    unittest.main()
