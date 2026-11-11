#!/usr/bin/env python3
"""HTTP-stub acceptance tests for the OpenObserve quota alert provisioner."""

from __future__ import annotations

import base64
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, unquote, urlsplit

from obs_alert_config import render_alert

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "deploy/o2"))
from provision import _validate_base_url  # noqa: E402

ENTRYPOINT = ROOT / "deploy/o2/provision.sh"
ALERT_SOURCE = ROOT / "deploy/openobserve/alerts/xflickr-quota-exhausted.json"
USERNAME = "stub-user"
PASSWORD = "stub-password"


class StubState:
    def __init__(self) -> None:
        self.alerts: list[dict[str, Any]] = []
        self.requests: list[dict[str, Any]] = []
        self.get_status = 200
        self.get_body: bytes | None = None
        self.array_response = False
        self.redirect = False
        self.post_status = 201


class StubHandler(BaseHTTPRequestHandler):
    state: StubState

    def log_message(self, format: str, *args: object) -> None:
        pass

    def _record(self, method: str) -> dict[str, Any] | None:
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b""
        body = json.loads(raw) if raw else None
        self.state.requests.append({
            "method": method, "path": self.path,
            "auth": self.headers.get("Authorization"),
            "content_type": self.headers.get("Content-Type"), "body": body,
        })
        return body

    def _respond(self, status: int, value: object) -> None:
        raw = value if isinstance(value, bytes) else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self) -> None:
        self._record("GET")
        if self.state.redirect:
            self.send_response(302)
            self.send_header("Location", "http://127.0.0.1/redirect-target")
            self.send_header("Content-Length", "0")
            self.end_headers()
        elif self.state.get_body is not None:
            self._respond(self.state.get_status, self.state.get_body)
        else:
            alerts = self.state.alerts
            stream_names = parse_qs(urlsplit(self.path).query).get("stream_name")
            if stream_names:
                alerts = [
                    alert for alert in alerts
                    if alert.get("stream_name") in stream_names
                ]
            body = alerts if self.state.array_response else {"list": alerts}
            self._respond(self.state.get_status, body)

    def do_POST(self) -> None:
        body = self._record("POST")
        if self.state.post_status // 100 == 2 and body is not None:
            self.state.alerts.append({**body, "alert_id": "alert/1"})
        self._respond(self.state.post_status, {"alert_id": "alert/1"})

    def do_PUT(self) -> None:
        body = self._record("PUT")
        alert_id = unquote(self.path.rsplit("/", 1)[-1])
        for index, alert in enumerate(self.state.alerts):
            if alert.get("alert_id", alert.get("id")) == alert_id:
                self.state.alerts[index] = {**(body or {}), "alert_id": alert_id}
                self._respond(200, {"alert_id": alert_id})
                return
        self._respond(404, {"error": "unknown alert"})


class StubServer:
    def __init__(self, state: StubState) -> None:
        handler = type("BoundStubHandler", (StubHandler,), {"state": state})
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    @property
    def url(self) -> str:
        host, port = self.server.server_address
        return f"http://{host}:{port}"

    def __enter__(self) -> StubServer:
        self.thread.start()
        return self

    def __exit__(self, *_: object) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


class OpenObserveProvisionTests(unittest.TestCase):
    def test_requires_https_for_non_loopback_url(self) -> None:
        cases = (
            ("http://127.0.0.1:15080", True),
            ("http://[::1]:15080", True),
            ("http://localhost:15080", True),
            ("https://openobserve.example.com", True),
            ("http://openobserve.example.com", False),
            ("http://192.168.10.18:5080", False),
        )
        for url, valid in cases:
            with self.subTest(url=url):
                if valid:
                    _validate_base_url(url)
                else:
                    with self.assertRaisesRegex(ValueError, "HTTPS"):
                        _validate_base_url(url)

    def run_provision(
        self, overrides: dict[str, str | None]
    ) -> subprocess.CompletedProcess[str]:
        env = {
            "PATH": os.environ["PATH"], "PYTHONDONTWRITEBYTECODE": "1",
            "OPENOBSERVE_PROVISION_URL": "http://127.0.0.1:15080",
            "OPENOBSERVE_ORG": "default", "OPENOBSERVE_STREAM": "xflickr",
            "OPENOBSERVE_USERNAME": USERNAME, "OPENOBSERVE_PASSWORD": PASSWORD,
            "OPENOBSERVE_ALERT_DESTINATION": "xflickr_telegram",
        }
        env.update(
            {key: value for key, value in overrides.items() if value is not None}
        )
        for key, value in overrides.items():
            if value is None:
                env.pop(key, None)
        return subprocess.run(
            [str(ENTRYPOINT)], cwd=ROOT, env=env, capture_output=True,
            text=True, timeout=15, check=False,
        )

    def expected_payload(self, stream: str, destination: str) -> dict[str, Any]:
        temp_root = ROOT / ".tmp"
        temp_root.mkdir(mode=0o700, parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(dir=temp_root) as directory:
            return render_alert(
                ALERT_SOURCE, Path(directory) / "alert.json", stream, destination
            )

    def assert_methods(self, state: StubState, expected: list[str]) -> None:
        self.assertEqual([request["method"] for request in state.requests], expected)

    def assert_no_secrets(self, result: subprocess.CompletedProcess[str]) -> None:
        for output in (result.stdout, result.stderr):
            self.assertNotIn(USERNAME, output)
            self.assertNotIn(PASSWORD, output)

    def test_creates_missing_quota_alert(self) -> None:
        state = StubState()
        state.array_response = True
        with StubServer(state) as server:
            result = self.run_provision({"OPENOBSERVE_PROVISION_URL": server.url})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assert_methods(state, ["GET", "POST"])
        get, post = state.requests
        auth = "Basic " + base64.b64encode(f"{USERNAME}:{PASSWORD}".encode()).decode()
        self.assertEqual(
            get["path"],
            "/api/v2/default/alerts",
        )
        self.assertEqual((get["auth"], post["auth"]), (auth, auth))
        self.assertEqual(
            (post["path"], post["content_type"]),
            ("/api/v2/default/alerts", "application/json"),
        )
        self.assertEqual(
            post["body"], self.expected_payload("xflickr", "xflickr_telegram")
        )
        self.assertEqual(len(state.alerts), 1)

    def test_updates_existing_quota_alert_without_duplicate(self) -> None:
        state = StubState()
        with StubServer(state) as server:
            first = self.run_provision({"OPENOBSERVE_PROVISION_URL": server.url})
            del state.alerts[0]["alert_id"]
            state.alerts[0]["id"] = "alert/1"
            second = self.run_provision({
                "OPENOBSERVE_PROVISION_URL": server.url,
                "OPENOBSERVE_ALERT_DESTINATION": "ops_destination",
            })
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assert_methods(state, ["GET", "POST", "GET", "PUT"])
        update = state.requests[3]
        self.assertEqual(update["path"], "/api/v2/default/alerts/alert%2F1")
        self.assertEqual(
            update["body"], self.expected_payload("xflickr", "ops_destination")
        )
        self.assertEqual(len(state.alerts), 1)

    def test_updates_existing_alert_when_stream_changes(self) -> None:
        state = StubState()
        with StubServer(state) as server:
            first = self.run_provision({"OPENOBSERVE_PROVISION_URL": server.url})
            second = self.run_provision({
                "OPENOBSERVE_PROVISION_URL": server.url,
                "OPENOBSERVE_STREAM": "custom_stream",
            })
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assert_methods(state, ["GET", "POST", "GET", "PUT"])
        update = state.requests[3]
        self.assertEqual(update["path"], "/api/v2/default/alerts/alert%2F1")
        self.assertEqual(
            update["body"],
            self.expected_payload("custom_stream", "xflickr_telegram"),
        )
        self.assertEqual(len(state.alerts), 1)
        self.assertEqual(state.alerts[0]["stream_name"], "custom_stream")

    def test_rejects_ambiguous_or_idless_alert(self) -> None:
        cases = (
            [{"name": "xflickr_quota_exhausted", "id": "one"},
             {"name": "xflickr_quota_exhausted", "id": "two"}],
            [{"name": "xflickr_quota_exhausted"}],
        )
        for alerts in cases:
            with self.subTest(alerts=alerts):
                state = StubState()
                state.alerts = alerts
                with StubServer(state) as server:
                    result = self.run_provision(
                        {"OPENOBSERVE_PROVISION_URL": server.url}
                    )
                self.assertNotEqual(result.returncode, 0)
                self.assert_methods(state, ["GET"])

    def test_rejects_invalid_configuration(self) -> None:
        cases = (
            {"OPENOBSERVE_USERNAME": None}, {"OPENOBSERVE_PASSWORD": None},
            {"OPENOBSERVE_ALERT_DESTINATION": None}, {"OPENOBSERVE_ORG": "bad org"},
            {"OPENOBSERVE_STREAM": "bad stream"},
            {"OPENOBSERVE_ALERT_DESTINATION": "bad destination"},
            {"OPENOBSERVE_PROVISION_URL": "http://user:pass@127.0.0.1:15080"},
            {"OPENOBSERVE_PROVISION_URL": "http://192.168.10.18:5080"},
            {"OPENOBSERVE_PROVISION_URL": "http://127.0.0.1:15080?x=1"},
            {"OPENOBSERVE_PROVISION_URL": "http://127.0.0.1:15080#fragment"},
            {"OPENOBSERVE_PROVISION_URL": "http://127.0.0.1:invalid"},
        )
        state = StubState()
        with StubServer(state) as server:
            for overrides in cases:
                with self.subTest(overrides=overrides):
                    result = self.run_provision(
                        {"OPENOBSERVE_PROVISION_URL": server.url, **overrides}
                    )
                    self.assertNotEqual(result.returncode, 0)
                    self.assert_no_secrets(result)
        self.assertEqual(state.requests, [])

    def test_reports_http_network_and_invalid_json_failures_without_secrets(
        self,
    ) -> None:
        cases = (
            "unauthorized", "invalid JSON", "invalid list", "create failure", "redirect"
        )
        for case in cases:
            with self.subTest(case=case):
                state = StubState()
                if case == "unauthorized":
                    state.get_status = 401
                    state.get_body = b'{"error":"' + PASSWORD.encode() + b'"}'
                elif case == "invalid JSON":
                    state.get_body = b"{invalid"
                elif case == "invalid list":
                    state.get_body = b'{"list":null}'
                elif case == "create failure":
                    state.post_status = 503
                else:
                    state.redirect = True
                with StubServer(state) as server:
                    result = self.run_provision(
                        {"OPENOBSERVE_PROVISION_URL": server.url}
                    )
                self.assertNotEqual(result.returncode, 0)
                self.assert_no_secrets(result)
                methods = ["GET", "POST"] if case == "create failure" else ["GET"]
                self.assert_methods(state, methods)

        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        result = self.run_provision(
            {"OPENOBSERVE_PROVISION_URL": f"http://127.0.0.1:{port}"}
        )
        self.assertNotEqual(result.returncode, 0)
        self.assert_no_secrets(result)


if __name__ == "__main__":
    unittest.main()
