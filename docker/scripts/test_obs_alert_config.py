#!/usr/bin/env python3
"""Offline acceptance checks for configured crawl-health alert behavior."""

import json
import os
import sqlite3
import subprocess
import tempfile
import textwrap
import unittest
from datetime import datetime, timezone
from pathlib import Path

from obs_alert_config import FRESHNESS_SECONDS, freshness_alert_needed, render_alert

ROOT = Path(__file__).resolve().parents[2]
ALERT_TEMPLATE = ROOT / "deploy/openobserve/alerts/xflickr-crawl-health-stale.json"
PROVISIONER = ROOT / "docker/scripts/obs-provision.sh"
NOW = 1_800_000_000.0


def event(name: str, age_seconds: int) -> dict[str, object]:
    return {"event": name, "_timestamp": (NOW - age_seconds) * 1_000_000}


class OpenObserveAlertConfigTests(unittest.TestCase):
    def test_scheduler_alert_uses_configured_stream_and_destination(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            output = Path(temp_dir) / "alert.json"
            alert = render_alert(ALERT_TEMPLATE, output, "crawl_metrics_custom", "ops_notifications")
            self.assertEqual(alert["name"], "xflickr_crawl_health_stale")
            self.assertEqual(alert["stream_type"], "logs")
            self.assertEqual(alert["stream_name"], "crawl_metrics_custom")
            self.assertFalse(alert["is_real_time"])
            self.assertEqual(alert["destinations"], ["ops_notifications"])
            self.assertIs(alert["enabled"], True)
            self.assertEqual(
                alert["description"],
                "Impact: Scheduler crawl-health telemetry is stale. "
                "Action: Check the scheduler, snapshot and OpenObserve ingestion.",
            )
            self.assertEqual(
                alert["trigger_condition"],
                {"period": 2, "operator": ">=", "threshold": 1, "frequency": 1, "silence": 15},
            )
            sql = alert["query_condition"]["sql"]
            self.assertIn('FROM "crawl_metrics_custom"', sql)
            self.assertIn("event='crawl.health'", sql)
            self.assertIn("now() - INTERVAL '90 seconds'", sql)
            self.assertIn("health_recent=0", sql)
            self.assertNotIn("crawl.health.key", sql)
            self.assertNotIn("key_recent", sql)
            self.assertEqual(json.loads(output.read_text(encoding="utf-8")), alert)

    def test_scheduler_freshness_boundaries(self) -> None:
        self.assertEqual(FRESHNESS_SECONDS, 90)
        self.assertTrue(freshness_alert_needed([], NOW))
        self.assertTrue(freshness_alert_needed([{"event": "crawl.health", "_timestamp": "invalid"}], NOW))
        self.assertTrue(freshness_alert_needed([event("crawl.health", 91)], NOW))
        self.assertTrue(
            freshness_alert_needed(
                [event("crawl.health", 91), event("crawl.health.key", 0)], NOW
            )
        )
        self.assertFalse(freshness_alert_needed([event("crawl.health", 90)], NOW))
        self.assertFalse(freshness_alert_needed([event("crawl.health", 89)], NOW))
        self.assertTrue(freshness_alert_needed([event("crawl.health.key", 0)], NOW))
        self.assertFalse(
            freshness_alert_needed(
                [event("crawl.health", 90), event("crawl.health.key", 0)], NOW
            )
        )

    def test_scheduler_timestamp_formats_remain_supported(self) -> None:
        timestamp = NOW - 90
        values = (
            timestamp,
            timestamp * 1_000,
            timestamp * 1_000_000,
            datetime.fromtimestamp(timestamp, timezone.utc).isoformat(),
        )
        for value in values:
            with self.subTest(value_type=type(value).__name__):
                self.assertFalse(
                    freshness_alert_needed(
                        [{"event": "crawl.health", "_timestamp": value}], NOW
                    )
                )

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
            db.execute(
                'CREATE TABLE crawl_metrics_custom (event TEXT, key_id INTEGER, '
                'calls_last_10min INTEGER, pending_items INTEGER, used INTEGER, '
                'remaining INTEGER, "limit" INTEGER, quota_waits_last_hour INTEGER, '
                'quota_waits_last_10min INTEGER)'
            )
            for filename, grants, limit, matches in cases:
                with self.subTest(filename=filename, grants=grants):
                    alert = render_alert(
                        ROOT / "deploy/openobserve/alerts" / filename,
                        Path(temp_dir) / "alert.json",
                        "crawl_metrics_custom",
                        "ops_notifications",
                    )
                    self.assertEqual(alert["stream_name"], "crawl_metrics_custom")
                    self.assertEqual(alert["destinations"], ["ops_notifications"])
                    self.assertTrue(alert["enabled"])
                    db.execute("DELETE FROM crawl_metrics_custom")
                    for name in ("crawl.health", "crawl.health.key"):
                        db.execute(
                            "INSERT INTO crawl_metrics_custom VALUES (?,?,?,?,?,?,?,?,?)",
                            (name, 1, grants, 2, grants, limit - grants, limit, 119000, 20000),
                        )
                    rows = db.execute(alert["query_condition"]["sql"]).fetchall()
                    self.assertEqual(len(rows), matches)

    def test_render_rejects_invalid_stream_and_destination(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            output = Path(temp_dir) / "alert.json"
            with self.assertRaises(ValueError):
                render_alert(ALERT_TEMPLATE, output, "bad stream", "ops_notifications")
            with self.assertRaises(ValueError):
                render_alert(ALERT_TEMPLATE, output, "crawl_metrics", "bad destination")

    def test_scheduler_alert_sql_fires_and_clears(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir, sqlite3.connect(":memory:") as db:
            alert = render_alert(ALERT_TEMPLATE, Path(temp_dir) / "alert.json", "custom_stream", "ops_notifications")
            sql = alert["query_condition"]["sql"].replace("now() - INTERVAL '90 seconds'", "?")
            db.execute('CREATE TABLE custom_stream (event TEXT, _timestamp REAL)')
            db.execute("INSERT INTO custom_stream VALUES (?, ?)", ("crawl.health", NOW - 91))
            self.assertEqual(len(db.execute(sql, (NOW - FRESHNESS_SECONDS,)).fetchall()), 1)

            db.execute("INSERT INTO custom_stream VALUES (?, ?)", ("crawl.health", NOW - 89))
            self.assertEqual(len(db.execute(sql, (NOW - FRESHNESS_SECONDS,)).fetchall()), 0)

    def test_provisioner_creates_and_updates_scheduler_alert(self) -> None:
        fake_curl = textwrap.dedent(
            """\
            #!/usr/bin/env python3
            import json
            import os
            import sys
            from pathlib import Path

            args = sys.argv[1:]
            method, output_path, data_path, url = "GET", None, None, ""
            write_out, fail = False, False
            index = 0
            while index < len(args):
                arg = args[index]
                if arg == "--request":
                    method = args[index + 1].upper(); index += 2
                elif arg == "--output":
                    output_path = args[index + 1]; index += 2
                elif arg == "--data-binary":
                    data_path = args[index + 1][1:]; index += 2
                elif arg == "--write-out":
                    write_out = True; index += 2
                elif arg == "--fail":
                    fail = True; index += 1
                elif arg.startswith("http://") or arg.startswith("https://"):
                    url = arg; index += 1
                else:
                    index += 1

            scenario = os.environ["SCENARIO"]
            body = json.loads(Path(data_path).read_text()) if data_path else None
            if "/streams?" in url:
                response = json.dumps([{"name": name} for name in (
                    "xflickr_logs", "xflickr_calls", "xflickr_metrics", "offline_stream"
                )])
            elif "/api/v2/" in url and "/alerts?" in url:
                rows = ([{"name": "xflickr_crawl_health_stale", "alert_id": "alert-123"}]
                        if scenario == "update" else [])
                response = json.dumps(rows)
            elif "/savedviews" in url or "/dashboards?" in url:
                response = "[]"
            else:
                response = "{}"

            status = 500 if body and body.get("name") == os.environ.get("FAIL_ALERT") else 200
            if method in ("POST", "PUT"):
                with open(os.environ["CURL_LOG"], "a", encoding="utf-8") as log:
                    log.write(json.dumps({"method": method, "url": url, "body": body}) + "\\n")
            if output_path and output_path != "/dev/null":
                Path(output_path).write_text(response, encoding="utf-8")
            elif not output_path and not write_out:
                sys.stdout.write(response)
            if write_out:
                sys.stdout.write(f"{status}\\n")
            if status >= 400 and fail:
                raise SystemExit(22)
            """
        )

        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            fake_bin = root / "bin"
            fake_bin.mkdir()
            curl = fake_bin / "curl"
            curl.write_text(fake_curl, encoding="utf-8")
            curl.chmod(0o755)
            env_file = root / "offline.env"
            env_file.write_text(
                "OPENOBSERVE_PROVISION_URL=http://openobserve.invalid\n"
                "OPENOBSERVE_ORG=default\n"
                "OPENOBSERVE_USERNAME=offline\n"
                "OPENOBSERVE_PASSWORD=offline\n"
                "OPENOBSERVE_STREAM=offline_stream\n"
                "OPENOBSERVE_ALERT_DESTINATION=offline_destination\n"
                "OPENOBSERVE_ALERT_TEMPLATE=offline_template\n",
                encoding="utf-8",
            )

            for scenario, expected_method, fail_alert in (
                ("create", "POST", ""),
                ("update", "PUT", ""),
                ("create", "POST", "xflickr_crawl_health_stale"),
            ):
                with self.subTest(scenario=scenario, fail_alert=fail_alert):
                    log_path = root / f"{scenario}-{bool(fail_alert)}.jsonl"
                    process_env = os.environ.copy()
                    process_env.update(
                        {
                            "PATH": f"{fake_bin}{os.pathsep}{process_env['PATH']}",
                            "SCENARIO": scenario,
                            "CURL_LOG": str(log_path),
                            "FAIL_ALERT": fail_alert,
                        }
                    )
                    result = subprocess.run(
                        [str(PROVISIONER), str(env_file)],
                        cwd=ROOT,
                        env=process_env,
                        capture_output=True,
                        text=True,
                        check=False,
                    )
                    if fail_alert:
                        self.assertNotEqual(result.returncode, 0, result.stderr)
                        continue

                    self.assertEqual(result.returncode, 0, result.stderr)
                    calls = [json.loads(line) for line in log_path.read_text(encoding="utf-8").splitlines()]
                    target = [
                        call for call in calls
                        if call["body"] and call["body"].get("name") == "xflickr_crawl_health_stale"
                    ]
                    self.assertEqual(len(target), 1)
                    self.assertEqual(target[0]["method"], expected_method)
                    payload = target[0]["body"]
                    self.assertEqual(payload["stream_name"], "offline_stream")
                    self.assertEqual(payload["destinations"], ["offline_destination"])
                    self.assertNotIn("crawl.health.key", payload["query_condition"]["sql"])


if __name__ == "__main__":
    unittest.main()
