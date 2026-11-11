#!/usr/bin/env python3
"""Provision the existing XFlickr quota alert in OpenObserve."""

from __future__ import annotations

import base64
import ipaddress
import json
import os
import re
import sys
import tempfile
from http.client import HTTPException
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "docker/scripts"))

from obs_alert_config import render_alert  # noqa: E402

IDENTIFIER_PATTERN = re.compile(r"^[A-Za-z0-9_-]+$")
ALERT_NAME = "xflickr_quota_exhausted"
ALERT_SOURCE = ROOT / "deploy/openobserve/alerts/xflickr-quota-exhausted.json"
TEMP_ROOT = ROOT / ".tmp"


class _RejectRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args: object, **kwargs: object) -> None:
        return None


def _validate_identifier(value: str, name: str) -> None:
    if not IDENTIFIER_PATTERN.fullmatch(value):
        raise ValueError(
            f"{name} may contain only letters, digits, underscores, and hyphens"
        )


def _validate_base_url(base_url: str) -> None:
    try:
        parts = urlsplit(base_url)
        hostname = parts.hostname
        _ = parts.port
    except ValueError:
        raise ValueError(
            "OPENOBSERVE_PROVISION_URL must be absolute HTTP(S) without "
            "userinfo, query, or fragment"
        ) from None

    if (
        parts.scheme not in {"http", "https"}
        or not parts.netloc
        or not hostname
        or "@" in parts.netloc
        or "?" in base_url
        or "#" in base_url
    ):
        raise ValueError(
            "OPENOBSERVE_PROVISION_URL must be absolute HTTP(S) without "
            "userinfo, query, or fragment"
        )

    if parts.scheme == "http":
        try:
            loopback = ipaddress.ip_address(hostname).is_loopback
        except ValueError:
            loopback = hostname.casefold() == "localhost"
        if not loopback:
            raise ValueError(
                "OPENOBSERVE_PROVISION_URL must use HTTPS unless the host is loopback"
            )


def request_json(
    method: str,
    url: str,
    username: str,
    password: str,
    payload: dict | None = None,
) -> object:
    """Send one authenticated JSON request and return its JSON response."""
    headers = {
        "Authorization": "Basic "
        + base64.b64encode(f"{username}:{password}".encode("utf-8")).decode("ascii")
    }
    data = None
    if payload is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(payload).encode("utf-8")

    request = Request(url, data=data, headers=headers, method=method)
    try:
        with build_opener(_RejectRedirect()).open(request, timeout=10) as response:
            return json.loads(response.read())
    except HTTPError as error:
        status = error.code
        error.close()
        raise RuntimeError(
            f"OpenObserve HTTP request failed with status {status}"
        ) from None
    except (HTTPException, URLError, TimeoutError, OSError):
        raise RuntimeError("OpenObserve network request failed") from None
    except (json.JSONDecodeError, UnicodeDecodeError):
        raise RuntimeError("OpenObserve returned invalid JSON") from None


def provision(
    base_url: str,
    org: str,
    username: str,
    password: str,
    stream: str,
    destination: str,
) -> None:
    """Create or update the configured quota alert without making a duplicate."""
    _validate_base_url(base_url)
    _validate_identifier(org, "OPENOBSERVE_ORG")
    if not username or not password:
        raise ValueError("OPENOBSERVE_USERNAME and OPENOBSERVE_PASSWORD are required")

    TEMP_ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="o2-provision-", dir=TEMP_ROOT) as temp_dir:
        output = Path(temp_dir) / "quota-alert.json"
        payload = render_alert(ALERT_SOURCE, output, stream, destination)

    alerts_url = f"{base_url.rstrip('/')}/api/v2/{quote(org, safe='')}/alerts"
    result = request_json(
        "GET",
        alerts_url,
        username,
        password,
    )

    if isinstance(result, list):
        alerts = result
    elif isinstance(result, dict) and isinstance(result.get("list"), list):
        alerts = result["list"]
    else:
        raise ValueError(
            "OpenObserve alert list response must be an array or contain a list"
        )

    matches = [
        alert
        for alert in alerts
        if isinstance(alert, dict) and alert.get("name") == ALERT_NAME
    ]
    if len(matches) > 1:
        raise ValueError(f"multiple OpenObserve alerts match {ALERT_NAME}")

    if matches:
        alert_id = next(
            (
                candidate
                for candidate in (matches[0].get("alert_id"), matches[0].get("id"))
                if isinstance(candidate, (str, int))
                and not isinstance(candidate, bool)
                and str(candidate) != ""
            ),
            None,
        )
        if alert_id is None:
            raise ValueError(f"OpenObserve alert {ALERT_NAME} has no usable ID")
        request_json(
            "PUT",
            f"{alerts_url}/{quote(str(alert_id), safe='')}",
            username,
            password,
            payload,
        )
        return

    request_json("POST", alerts_url, username, password, payload)


def main() -> int:
    """Read process configuration and report concise provisioning failures."""
    try:
        provision(
            os.environ.get("OPENOBSERVE_PROVISION_URL", "http://127.0.0.1:15080"),
            os.environ.get("OPENOBSERVE_ORG", "default"),
            os.environ.get("OPENOBSERVE_USERNAME", ""),
            os.environ.get("OPENOBSERVE_PASSWORD", ""),
            os.environ.get("OPENOBSERVE_STREAM", "xflickr"),
            os.environ.get("OPENOBSERVE_ALERT_DESTINATION", ""),
        )
    except (OSError, RuntimeError, ValueError) as error:
        print(f"OpenObserve provisioning failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
