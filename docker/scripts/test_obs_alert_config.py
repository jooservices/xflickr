#!/usr/bin/env python3
"""Offline acceptance checks for configured crawl-health alert behavior."""

import json
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
