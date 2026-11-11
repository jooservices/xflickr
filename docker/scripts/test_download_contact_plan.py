from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import download_contact_plan


class ContactDownloadPlanTests(unittest.TestCase):
    def test_fixture_uses_adapter_query_and_spreads_four_hundred_thousand_photos(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            repository = root / download_contact_plan.REPOSITORY_PATH
            repository.parent.mkdir(parents=True)
            repository.write_text(
                'case "contact":\\n'
                "rows, err = pool.Query(ctx, `SELECT p.id FROM photos p "
                "WHERE p.owner_nsid=$2 AND p.tombstoned_at IS NULL "
                "ORDER BY p.id LIMIT 501`, userID, subject)\\n",
                encoding="utf-8",
            )

            sql = download_contact_plan.fixture_sql(
                root, 8_000_000_000_000_000_000, "owner@N00"
            )

        self.assertIn("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)", sql)
        self.assertIn("p.owner_nsid='owner@N00'", sql)
        self.assertIn("ORDER BY p.id LIMIT 501", sql)
        self.assertIn("generate_series(0, 15)", sql)
        self.assertIn("generate_series(1, 400000)", sql)
        self.assertIn("(n-1)%16=0", sql)

    def test_contact_plan_requires_index_and_bounded_reads(self) -> None:
        document = {
            "Plan": {
                "Node Type": "Limit",
                "Plans": [
                    {
                        "Node Type": "Index Scan",
                        "Relation Name": "photos_p00",
                        "Index Name": "photos_p00_owner_download_id_idx",
                        "Actual Rows": 501,
                        "Actual Loops": 1,
                    }
                ],
            },
            "Execution Time": 1.8,
            "Planning Time": 6.1,
        }

        result = download_contact_plan.inspect_contact_plan(document)

        self.assertEqual(result["owner_id_indexes"], ["photos_p00_owner_download_id_idx"])
        self.assertEqual(result["index_rows"], 501)
        self.assertEqual(result["execution_ms"], 1.8)

    def test_contact_plan_rejects_photo_sequential_scan(self) -> None:
        document = {
            "Plan": {
                "Node Type": "Limit",
                "Plans": [
                    {
                        "Node Type": "Seq Scan",
                        "Relation Name": "photos_p00",
                        "Actual Rows": 400_000,
                        "Actual Loops": 1,
                    }
                ],
            }
        }

        with self.assertRaisesRegex(
            RuntimeError, "contact plan has photo sequential scans"
        ):
            download_contact_plan.inspect_contact_plan(document)

    def test_contact_plan_rejects_an_index_that_does_not_match_owner_and_id(self) -> None:
        document = {
            "Plan": {
                "Node Type": "Limit",
                "Plans": [
                    {
                        "Node Type": "Index Scan",
                        "Relation Name": "photos_p00",
                        "Index Name": "photos_p00_pkey",
                        "Actual Rows": 501,
                        "Actual Loops": 1,
                    }
                ],
            }
        }

        with self.assertRaisesRegex(
            RuntimeError, "did not use the owner-and-ID index"
        ):
            download_contact_plan.inspect_contact_plan(document)

    def test_contact_plan_rejects_unbounded_owner_index_reads(self) -> None:
        document = {
            "Plan": {
                "Node Type": "Limit",
                "Plans": [
                    {
                        "Node Type": "Index Scan",
                        "Relation Name": "photos_p00",
                        "Index Name": "photos_p00_owner_download_id_idx",
                        "Actual Rows": 400_000,
                        "Actual Loops": 1,
                    }
                ],
            }
        }

        with self.assertRaisesRegex(RuntimeError, "read 400000 index rows"):
            download_contact_plan.inspect_contact_plan(document)


if __name__ == "__main__":
    unittest.main()

