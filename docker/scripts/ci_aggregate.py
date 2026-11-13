#!/usr/bin/env python3
"""Evaluate required and path-filtered CI job results."""

from __future__ import annotations

import json
import os
import sys
from collections.abc import Mapping, Set


def aggregate(
    results: Mapping[str, str],
    required: Set[str],
    optional: Set[str],
) -> list[str]:
    """Return failed, cancelled, missing or unexpectedly skipped jobs."""
    failures = []
    for job in sorted(required | optional):
        result = results.get(job)
        if job in required and result != "success":
            failures.append(f"{job}: {result or 'missing'} (required)")
        elif job in optional and result not in {"success", "skipped"}:
            failures.append(f"{job}: {result or 'missing'} (optional jobs may only succeed or skip)")
    return failures


def required_jobs(flags: Mapping[str, str]) -> set[str]:
    """Select expected job conclusions from the changed-path outputs."""
    required = {"changes", "static"}
    groups = {
        "go": {"lint", "go-test", "go-coverage"},
        "web": {"web-lint", "web-test", "web-coverage"},
        "integration": {"integration", "integration-count"},
        "stale": {"stale-check"},
        "security_go": {"security-backend"},
        "security_web": {"security-web"},
    }
    for flag, jobs in groups.items():
        if flags.get(flag) == "true":
            required.update(jobs)
    return required


def main() -> int:
    try:
        needs = json.loads(os.environ["CI_NEEDS"])
        results = {name: value["result"] for name, value in needs.items()}
        flags = {
            "go": os.environ.get("CI_GO", "false"),
            "web": os.environ.get("CI_WEB", "false"),
            "integration": os.environ.get("CI_INTEGRATION", "false"),
            "stale": os.environ.get("CI_STALE", "false"),
            "security_go": os.environ.get("CI_SECURITY_GO", "false"),
            "security_web": os.environ.get("CI_SECURITY_WEB", "false"),
        }
        required = required_jobs(flags)
        all_jobs = {
            "changes", "static", "lint", "web-lint", "web-test", "web-coverage",
            "go-test", "go-coverage", "integration", "integration-count", "stale-check",
            "security-backend", "security-web",
        }
        optional = all_jobs - required
    except (KeyError, TypeError, json.JSONDecodeError) as error:
        print(f"CI aggregator failed to read job results: {error}", file=sys.stderr)
        return 1
    failures = aggregate(results, required, optional)
    if failures:
        print("CI jobs failed:\n" + "\n".join(f"- {failure}" for failure in failures), file=sys.stderr)
        return 1
    print("CI aggregator passed: every required job succeeded")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
