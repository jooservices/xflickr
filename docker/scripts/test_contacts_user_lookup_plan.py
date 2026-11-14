#!/usr/bin/env python3
"""Unit tests for the contacts lookup plan's worktree database guard."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import contacts_user_lookup_plan


class ContactsUserLookupPlanTest(unittest.TestCase):
    def test_AC5_uses_the_tree_database_and_non_root_user(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / ".env").write_text(
                "DB_NAME=wt_jooxf522\nDB_DATABASE=wt_jooxf522\n"
                f"XFLICKR_WORKTREE_PATH={root.resolve()}\n"
                "DB_HOST=127.0.0.1\nDB_PORT=15435\nDB_USER=wt_jooxf522\nDB_PASSWORD=fixture\n"
            )
            with patch.object(contacts_user_lookup_plan, "ROOT", root):
                environment = contacts_user_lookup_plan.postgres_environment("jooxf522")

        self.assertEqual(environment["PGDATABASE"], "wt_jooxf522")
        self.assertEqual(environment["PGUSER"], "wt_jooxf522")
        self.assertNotEqual(environment["PGUSER"], "postgres")

    def test_refuses_the_live_database(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / ".env").write_text(
                "DB_NAME=xflickr\nDB_DATABASE=xflickr\n"
                f"XFLICKR_WORKTREE_PATH={root.resolve()}\n"
                "DB_HOST=127.0.0.1\nDB_PORT=15435\nDB_USER=postgres\nDB_PASSWORD=fixture\n"
            )
            with patch.object(contacts_user_lookup_plan, "ROOT", root):
                with self.assertRaises(ValueError):
                    contacts_user_lookup_plan.postgres_environment("jooxf522")


if __name__ == "__main__":
    unittest.main()
