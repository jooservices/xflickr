#!/usr/bin/env python3
"""Run the CI verification stages in their canonical cheap-first order."""

from __future__ import annotations

import subprocess
import sys

from verify_stages import VERIFY_STAGES


def main() -> int:
    for stage in VERIFY_STAGES:
        print(f"\n==> make {stage}", flush=True)
        result = subprocess.run(["make", "--no-print-directory", stage], check=False)
        if result.returncode != 0:
            print(f"Verification stopped at {stage} (exit {result.returncode}).", file=sys.stderr)
            return result.returncode
    print("\nAll CI verification stages passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
