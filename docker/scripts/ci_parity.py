#!/usr/bin/env python3
"""Check that CI and local verification use the same Make targets."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

from verify_stages import VERIFY_STAGES

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/ci.yml"
ALLOWLIST = ROOT / "docker/scripts/ci-parity-allowlist.json"
RUN_LINE = re.compile(r"^\s*(?:-\s*)?run:\s*(.*?)\s*$")
MAKE_TARGET = re.compile(r"^make\s+([a-z][a-z0-9-]*)\b")


def workflow_commands(text: str) -> set[str]:
    """Return Make target names invoked directly by the CI workflow."""
    commands: set[str] = set()
    in_run_block = False
    run_indent = 0
    for line in text.splitlines():
        run = RUN_LINE.match(line)
        if run is not None and run[1] == "|":
            in_run_block = True
            run_indent = len(line) - len(line.lstrip())
            continue
        if run is not None:
            record_command(run[1], commands)
            continue
        if in_run_block:
            indent = len(line) - len(line.lstrip())
            if line.strip() and indent <= run_indent:
                in_run_block = False
            else:
                if line.strip() and not line.strip().startswith("#"):
                    record_command(line.strip(), commands)
        action = re.search(r"\buses:\s*\.\/\.github/actions/([a-z0-9-]+)", line)
        if action is not None:
            commands.add(action[1])
        if "uses: actions/upload-artifact@" in line:
            commands.add("artifact-upload")
        if "uses: actions/download-artifact@" in line:
            commands.add("artifact-download")
    return commands


def record_command(command: str, commands: set[str]) -> None:
    """Record a Make target or the first token of an unsupported shell command."""
    target = MAKE_TARGET.match(command)
    if target is not None:
        commands.add(target[1])
    elif command:
        commands.add(command.split(maxsplit=1)[0])


def check_parity(
    workflow_text: str,
    verify_stages: list[str] | tuple[str, ...],
    allowlist: dict[str, list[str]],
) -> None:
    """Reject unapproved command differences between CI and make verify."""
    workflow_only = workflow_commands(workflow_text) - set(verify_stages)
    local_only = set(verify_stages) - workflow_commands(workflow_text)
    allowed_github = set(allowlist.get("github_only", []))
    allowed_local = set(allowlist.get("local_only", []))
    failures = sorted(workflow_only - allowed_github) + sorted(local_only - allowed_local)
    if failures:
        raise ValueError("CI parity mismatch: " + ", ".join(failures))


def workflow_configuration(text: str) -> None:
    """Validate the workflow's required triggers and ref cancellation."""
    lines = text.splitlines()
    top_level = {line.strip() for line in lines if line and not line[0].isspace()}
    if "on:" not in top_level:
        raise ValueError("workflow must declare on triggers")
    if not re.search(r"(?ms)^on:\s*\n(?:(?!\S).)*?^  push:\s*\n(?:    .*\n)*?    branches:\s*\[develop, master\]", text):
        raise ValueError("push branches must be develop and master")
    if not re.search(r"(?m)^  pull_request:\s*$", text):
        raise ValueError("pull_request must include all pull requests")
    if not re.search(r"(?ms)^concurrency:\s*\n(?:(?!\S).)*?^  group:.*github\.ref", text):
        raise ValueError("concurrency group must identify the PR or ref")
    if not re.search(r"(?m)^  cancel-in-progress:\s*true\s*$", text):
        raise ValueError("concurrency must cancel older runs")


def main() -> int:
    try:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        allowlist = json.loads(ALLOWLIST.read_text(encoding="utf-8"))
        workflow_configuration(workflow)
        check_parity(workflow, VERIFY_STAGES, allowlist)
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"CI parity check failed: {error}", file=sys.stderr)
        return 1
    print("CI parity check passed: workflow and make verify stages match")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
