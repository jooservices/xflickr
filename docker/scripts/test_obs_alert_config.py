#!/usr/bin/env python3
"""Offline acceptance checks for configured crawl-health alert behavior."""

import json
import sqlite3
import tempfile
import unittest
from pathlib import Path

from obs_alert_config import FRESHNESS_SECONDS, freshness_alert_needed, render_alert

ROOT = Path(__file__).resolve().parents[2]
ALERT_TEMPLATE = ROOT / "deploy/openobserve/alerts/xflickr-crawl-health-stale.json"
NOW = 1_800_000_000.0


def event(name: str, age_seconds: int) -> dict[str, object]:
    return {"event": name, "_timestamp": (NOW - age_seconds) * 1_000_000}


class OpenObserveAlertConfigTests(unittest.TestCase):
    def test_alert_uses_configured_stream_destination_and_is_enabled(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            output = Path(temp_dir) / "alert.json"
            alert = render_alert(ALERT_TEMPLATE, output, "crawl_metrics_custom", "ops_notifications")
            self.assertEqual(alert["stream_name"], "crawl_metrics_custom")
            self.assertEqual(alert["destinations"], ["ops_notifications"])
            self.assertTrue(alert["enabled"])
            sql = alert["query_condition"]["sql"]
            self.assertIn('FROM "crawl_metrics_custom"', sql)
            self.assertIn("now() - INTERVAL '90 seconds'", sql)
            self.assertIn("crawl.health", sql)
            self.assertIn("crawl.health.key", sql)
            self.assertEqual(json.loads(output.read_text(encoding="utf-8")), alert)

    def test_granted_call_alerts_ignore_waits_and_keep_configured_delivery(self) -> None:
        cases = [
            ("xflickr-no-calls-pending.json", 0, 100, 1),
            ("xflickr-no-calls-pending.json", 1, 100, 0),
            ("xflickr-quota-near-limit.json", 89, 100, 0),
            ("xflickr-quota-near-limit.json", 90, 100, 1),
            ("xflickr-quota-exhausted.json", 99, 100, 0),
            ("xflickr-quota-exhausted.json", 100, 100, 1),
        ]
        with tempfile.TemporaryDirectory() as temp_dir, sqlite3.connect(":memory:") as db:
            db.execute('CREATE TABLE crawl_metrics_custom (event TEXT, key_id INTEGER, '
                       'calls_last_10min INTEGER, pending_items INTEGER, used INTEGER, '
                       'remaining INTEGER, "limit" INTEGER, quota_waits_last_hour INTEGER, '
                       'quota_waits_last_10min INTEGER)')
            for filename, grants, limit, matches in cases:
                with self.subTest(filename=filename, grants=grants):
                    alert = render_alert(ROOT / "deploy/openobserve/alerts" / filename,
                                         Path(temp_dir) / "alert.json",
                                         "crawl_metrics_custom", "ops_notifications")
                    self.assertEqual(alert["stream_name"], "crawl_metrics_custom")
                    self.assertEqual(alert["destinations"], ["ops_notifications"])
                    self.assertTrue(alert["enabled"])
                    db.execute('DELETE FROM crawl_metrics_custom')
                    for name in ("crawl.health", "crawl.health.key"):
                        db.execute('INSERT INTO crawl_metrics_custom VALUES (?,?,?,?,?,?,?,?,?)',
                                   (name, 1, grants, 2, grants, limit-grants, limit, 119000, 20000))
                    rows = db.execute(alert["query_condition"]["sql"]).fetchall()
                    self.assertEqual(len(rows), matches)

    def test_missing_stream_or_destination_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            output = Path(temp_dir) / "alert.json"
            with self.assertRaises(ValueError):
                render_alert(ALERT_TEMPLATE, output, "bad stream", "ops_notifications")
            with self.assertRaises(ValueError):
                render_alert(ALERT_TEMPLATE, output, "crawl_metrics", "bad destination")

    def test_no_data_and_each_missing_or_stale_event_type_alert(self) -> None:
        self.assertEqual(FRESHNESS_SECONDS, 90)
        self.assertTrue(freshness_alert_needed([], NOW))
        self.assertTrue(freshness_alert_needed([event("crawl.health", 30)], NOW))
        self.assertTrue(freshness_alert_needed([event("crawl.health.key", 30)], NOW))
        self.assertTrue(
            freshness_alert_needed(
                [event("crawl.health", 91), event("crawl.health.key", 30)], NOW
            )
        )
        self.assertTrue(
            freshness_alert_needed(
                [event("crawl.health", 30), event("crawl.health.key", 91)], NOW
            )
        )

    def test_both_event_types_fresh_suppress_alert(self) -> None:
        self.assertFalse(
            freshness_alert_needed(
                [event("crawl.health", 90), event("crawl.health.key", 89)], NOW
            )
        )


if __name__ == "__main__":
    unittest.main()
