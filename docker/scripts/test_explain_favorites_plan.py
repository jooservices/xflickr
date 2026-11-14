#!/usr/bin/env python3
"""Unit tests for the favorites plan's worktree database guard."""

from __future__ import annotations

import importlib.util
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("explain-favorites-plan.py")
SPEC = importlib.util.spec_from_file_location("explain_favorites_plan", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("cannot load favorites plan script")
EXPLAIN_FAVORITES_PLAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EXPLAIN_FAVORITES_PLAN)


class ExplainFavoritesPlanTest(unittest.TestCase):
    def test_AC5_accepts_the_tree_database(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            parser_path = root / "docker/scripts/worktree_env.py"
            parser_path.parent.mkdir(parents=True)
            shutil.copyfile(SCRIPT.with_name("worktree_env.py"), parser_path)
            (root / ".env").write_text(
                "DB_NAME=wt_jooxf_522\nDB_DATABASE=wt_jooxf_522\n"
                "DB_HOST=127.0.0.1\nDB_PORT=15435\nDB_USER=wt_jooxf_522\nDB_PASSWORD=fixture\n"
            )
            with patch.object(EXPLAIN_FAVORITES_PLAN, "ROOT", root):
                settings = EXPLAIN_FAVORITES_PLAN.database_settings()

        self.assertEqual(settings["DB_NAME"], "wt_jooxf_522")
        self.assertEqual(settings["DB_USER"], "wt_jooxf_522")

    def test_refuses_the_shared_test_database(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            parser_path = root / "docker/scripts/worktree_env.py"
            parser_path.parent.mkdir(parents=True)
            shutil.copyfile(SCRIPT.with_name("worktree_env.py"), parser_path)
            (root / ".env").write_text(
                "DB_NAME=xflickr_test\nDB_HOST=127.0.0.1\nDB_PORT=15435\n"
                "DB_USER=test\nDB_PASSWORD=fixture\n"
            )
            with patch.object(EXPLAIN_FAVORITES_PLAN, "ROOT", root):
                with self.assertRaises(RuntimeError):
                    EXPLAIN_FAVORITES_PLAN.database_settings()


if __name__ == "__main__":
    unittest.main()
