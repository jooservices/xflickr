#!/usr/bin/env python3
"""Render OpenObserve alert templates and evaluate crawl-health freshness."""

from __future__ import annotations

import argparse
import json
import re
from datetime import datetime
from pathlib import Path
from typing import Any

STREAM_PLACEHOLDER = "{{OPENOBSERVE_STREAM}}"
FRESHNESS_SECONDS = 90
HEALTH_EVENTS = ("crawl.health", "crawl.health.key")
STREAM_PATTERN = re.compile(r"^[A-Za-z0-9_-]+$")


def render_alert(source: Path, output: Path, stream: str, destination: str) -> dict[str, Any]:
    if not STREAM_PATTERN.fullmatch(stream):
        raise ValueError("OpenObserve stream names may contain only letters, digits, underscores, and hyphens")
    if not STREAM_PATTERN.fullmatch(destination):
        raise ValueError("OpenObserve alert destination names may contain only letters, digits, underscores, and hyphens")

    alert = json.loads(source.read_text(encoding="utf-8"))

    def replace_placeholder(value: Any) -> Any:
        if isinstance(value, str):
            return value.replace(STREAM_PLACEHOLDER, stream)
        if isinstance(value, list):
            return [replace_placeholder(item) for item in value]
        if isinstance(value, dict):
            return {key: replace_placeholder(item) for key, item in value.items()}
        return value

    alert = replace_placeholder(alert)
    alert["destinations"] = [destination]
    if alert.get("enabled") is not True:
        raise ValueError("OpenObserve alert must be enabled")
    if STREAM_PLACEHOLDER in json.dumps(alert):
        raise ValueError("OpenObserve alert has an unresolved configured-stream placeholder")

    output.write_text(json.dumps(alert, separators=(",", ":")) + "\n", encoding="utf-8")
    return alert


def _timestamp_seconds(value: Any) -> float | None:
    if isinstance(value, (int, float)):
        timestamp = float(value)
        if timestamp > 100_000_000_000_000:  # OpenObserve timestamps are commonly microseconds.
            return timestamp / 1_000_000
        if timestamp > 100_000_000_000:  # Also accept millisecond epoch fixtures.
            return timestamp / 1_000
        return timestamp
    if isinstance(value, str):
        try:
            return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
        except ValueError:
            return None
    return None


def freshness_alert_needed(events: list[dict[str, Any]], now_seconds: float) -> bool:
    """Mirror the alert's 90-second rule; a missing event type is stale."""
    for event_name in HEALTH_EVENTS:
        recent = any(
            event.get("event") == event_name
            and (timestamp := _timestamp_seconds(event.get("_timestamp"))) is not None
            and timestamp >= now_seconds - FRESHNESS_SECONDS
            for event in events
        )
        if not recent:
            return True
    return False


def _main() -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    render_parser = commands.add_parser("render")
    render_parser.add_argument("source", type=Path)
    render_parser.add_argument("output", type=Path)
    render_parser.add_argument("stream")
    render_parser.add_argument("destination")
    stream_parser = commands.add_parser("stream")
    stream_parser.add_argument("alert", type=Path)
    args = parser.parse_args()

    if args.command == "render":
        render_alert(args.source, args.output, args.stream, args.destination)
    else:
        alert = json.loads(args.alert.read_text(encoding="utf-8"))
        print(alert["stream_name"])
    return 0


if __name__ == "__main__":
    raise SystemExit(_main())
