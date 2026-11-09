"""Tests for the synthetic work-items loader argument validation."""

from __future__ import annotations

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("synthetic-work-items.sh")


class SyntheticWorkItemsArgumentsTest(unittest.TestCase):
    def test_non_integer_counts_fail_before_calling_psql(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            temp = Path(directory)
            psql = temp / "psql"
            marker = temp / "psql-called"
            psql.write_text("#!/bin/sh\ntouch \"$PSQL_MARKER\"\n", encoding="utf-8")
            psql.chmod(0o755)
            env = os.environ.copy()
            env["PATH"] = f"{temp}{os.pathsep}{env['PATH']}"
            env["PSQL_MARKER"] = str(marker)

            for pending, total in (("many", "10"), ("1", "2.5"), ("-1", "10")):
                with self.subTest(pending=pending, total=total):
                    result = subprocess.run(
                        [str(SCRIPT), "unused-database-url", pending, total],
                        check=False,
                        capture_output=True,
                        text=True,
                        env=env,
                    )
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("row counts must be non-negative integers", result.stderr)
                    self.assertFalse(marker.exists(), "psql ran for invalid row counts")


if __name__ == "__main__":
    unittest.main()
