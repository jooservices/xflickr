#!/usr/bin/env python3
"""Unit tests for the non-CI metric-series plan measurement helper."""

from __future__ import annotations

import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import metrics_series_plan


class MetricsSeriesPlanTest(unittest.TestCase):
    def test_AC5_accepts_the_tree_database_for_plan_measurement(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / ".env").write_text(
                "DB_NAME=wt_jooxf_522\nDB_DATABASE=wt_jooxf_522\n"
                f"XFLICKR_WORKTREE_PATH={root.resolve()}\n"
                "DB_HOST=127.0.0.1\nDB_PORT=15435\nDB_USER=wt_jooxf_522\nDB_PASSWORD=fixture\n"
            )
            with patch("metrics_series_plan.subprocess.run") as run:
                run.return_value.returncode = 0
                values = metrics_series_plan.worktree_env(root)

        self.assertEqual(values["DB_NAME"], "wt_jooxf_522")
        self.assertEqual(values["DB_USER"], "wt_jooxf_522")

    def test_labels_hash_uses_sorted_canonical_json(self) -> None:
        labels = {"role": "scheduler", "queue": "default", "node": "n1"}
        expected = hashlib.sha256(
            b'{"node":"n1","queue":"default","role":"scheduler"}'
        ).hexdigest()
        self.assertEqual(metrics_series_plan.labels_hash(labels), expected)
        self.assertEqual(
            metrics_series_plan.labels_hash(labels),
            metrics_series_plan.labels_hash(dict(reversed(list(labels.items())))),
        )

    def test_inspect_plan_finds_unique_index_recursively(self) -> None:
        plan = json.loads(
            '[{"Plan":{"Node Type":"Limit","Plans":[{"Node Type":"Index Scan",'
            '"Index Name":"metric_samples_unique"}]}}]'
        )
        self.assertEqual(metrics_series_plan.inspect_plan(plan), (True, []))

    def test_inspect_plan_rejects_sequential_scans(self) -> None:
        plan = [{"Plan": {"Node Type": "Parallel Seq Scan"}}]
        self.assertEqual(metrics_series_plan.inspect_plan(plan), (False, ["Parallel Seq Scan"]))

    def test_query_fixture_rolls_back_after_explain_analyze(self) -> None:
        sql = metrics_series_plan.query_sql()
        self.assertIn("generate_series(0, 400000)", sql)
        self.assertIn("sample >= 399941", sql)
        self.assertIn("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)", sql)
        self.assertTrue(sql.rstrip().endswith("ROLLBACK;"))
        self.assertEqual(metrics_series_plan.FIXTURE_ROWS, 400_001)


if __name__ == "__main__":
    unittest.main()
