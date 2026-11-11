from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import photolane_plan  # noqa: E402


class PhotoLanePlanTests(unittest.TestCase):
    def test_query_uses_shared_confirmation_sql_and_fourth_limit_parameter(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            selector_path = root / photolane_plan.SELECTOR_PATH
            selector_path.parent.mkdir(parents=True)
            selector_path.write_text(
                "SELECT __PHOTO_SIZES_CONFIRMED__, __PHOTO_SIZES_CONFIRMED__ "
                "FROM photos WHERE id < $4;\n",
                encoding="utf-8",
            )
            fragment_path = root / photolane_plan.CONFIRMED_FRAGMENT_PATH
            fragment_path.write_text("EXISTS (SELECT 1 FROM photo_account_sizes)", encoding="utf-8")

            sql = photolane_plan.query_sql(root, 100, "owner@N00", "owner@example.test")

        self.assertNotIn("__PHOTO_SIZES_CONFIRMED__", sql)
        self.assertEqual(sql.count("EXISTS (SELECT 1 FROM photo_account_sizes)"), 2)
        self.assertIn("PREPARE photolane_plan (integer, integer, text[], integer)", sql)
        self.assertIn("EXECUTE photolane_plan(1, 20, ARRAY[]::text[], 50)", sql)

    def test_fixture_gives_one_owner_four_hundred_thousand_keyset_photos(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            selector_path = root / photolane_plan.SELECTOR_PATH
            selector_path.parent.mkdir(parents=True)
            selector_path.write_text("SELECT __PHOTO_SIZES_CONFIRMED__;", encoding="utf-8")
            fragment_path = root / photolane_plan.CONFIRMED_FRAGMENT_PATH
            fragment_path.parent.mkdir(parents=True, exist_ok=True)
            fragment_path.write_text("true", encoding="utf-8")

            sql = photolane_plan.query_sql(root, 100, "owner@N00", "owner@example.test")

        self.assertIn("generate_series(1, 400000)", sql)
        self.assertIn("'owner@N00', now() - n * interval '1 second'", sql)
        self.assertIn("photo_lane_cursor:owner@N00", sql)

    def test_plan_inspection_requires_order_index_and_rejects_photo_seq_scan(self) -> None:
        plan = [
            {
                "Plan": {
                    "Node Type": "Limit",
                    "Plans": [
                        {
                            "Node Type": "Index Scan",
                            "Relation Name": "photos_p00",
                            "Index Name": "photos_p00_photo_lane_order_idx",
                        }
                    ],
                }
            }
        ]

        self.assertEqual(photolane_plan.inspect_plan(plan), [])
        self.assertEqual(
            photolane_plan.photo_lane_order_indexes(plan),
            ["photos_p00_photo_lane_order_idx"],
        )
        plan[0]["Plan"]["Plans"][0]["Node Type"] = "Seq Scan"
        plan[0]["Plan"]["Plans"][0].pop("Index Name")
        self.assertEqual(photolane_plan.inspect_plan(plan), ["photos_p00"])
        self.assertEqual(photolane_plan.photo_lane_order_indexes(plan), [])


if __name__ == "__main__":
    unittest.main()
