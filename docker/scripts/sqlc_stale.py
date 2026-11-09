#!/usr/bin/env python3
"""Helpers that prove sqlc output tracks migrations under the pinned go tool."""

from __future__ import annotations

import subprocess
from pathlib import Path


def run_sqlc_generate(backend_dir: Path, config_file: Path) -> None:
    """Run the module-pinned sqlc against an alternate config file."""
    completed = subprocess.run(
        ["go", "tool", "sqlc", "--file", str(config_file), "generate"],
        cwd=backend_dir,
        check=False,
        capture_output=True,
        text=True,
    )
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout or "").strip()
        raise RuntimeError(f"sqlc generate failed: {detail}")


def working_tree_is_dirty(repo_dir: Path) -> bool:
    """Return True when git reports any tracked or untracked change."""
    completed = subprocess.run(
        ["git", "-C", str(repo_dir), "status", "--porcelain", "--untracked-files=all"],
        check=True,
        capture_output=True,
        text=True,
    )
    return bool(completed.stdout.strip())
