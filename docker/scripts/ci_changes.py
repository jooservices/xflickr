#!/usr/bin/env python3
"""Select CI jobs from changed paths and publish shared tool pins."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

from check_tool_versions import ROOT, parse_versions, write_github_output
from integration_shards import group_names, integration_module_shards, integration_test_packages


def classify_paths(paths: list[str] | tuple[str, ...]) -> dict[str, bool]:
    """Return whether Go, web, integration, generation and audit jobs apply."""
    paths = tuple(path.replace("\\", "/") for path in paths)
    global_change = any(
        path.startswith((".github/actions/", ".github/workflows/"))
        or path in {"Makefile", "Dockerfile", ".tool-versions"}
        for path in paths
    )
    go = global_change or any(
        path.startswith(("backend/", "docker/scripts/", "contracts/openapi/")) or path == ".golangci.yml"
        for path in paths
    )
    web = global_change or any(path.startswith(("web/", "contracts/openapi/", "docker/scripts/")) for path in paths)
    integration = any(path.startswith(("backend/", "docker/scripts/", "contracts/openapi/")) for path in paths)
    stale = any(
        path.startswith(("backend/", "contracts/openapi/", "web/src/shared/api/gen/", "web/src/shared/api/zod/"))
        or path in {"Makefile", "Dockerfile"}
        for path in paths
    )
    security_go = go
    security_web = web
    return {
        "go": go,
        "web": web,
        "integration": integration,
        "stale": stale,
        "security_go": security_go,
        "security_web": security_web,
    }


def changed_paths(base: str) -> list[str]:
    """List paths changed against the event base or the previous commit."""
    if not base or set(base) == {"0"}:
        arguments = ["git", "diff", "--name-only", "HEAD^", "HEAD"]
    else:
        arguments = ["git", "diff", "--name-only", f"{base}...HEAD"]
    result = subprocess.run(arguments, cwd=ROOT, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise RuntimeError("git diff for CI path filters failed: " + result.stderr.strip())
    return [line for line in result.stdout.splitlines() if line]


def outputs_for(paths: list[str]) -> dict[str, str]:
    """Build job-selection and tool-version outputs for the workflow."""
    selection = classify_paths(paths)
    outputs = {key: str(value).lower() for key, value in selection.items()}
    pins = parse_versions((ROOT / ".tool-versions").read_text(encoding="utf-8"))
    outputs.update(
        {
            "go_version": pins["go"],
            "node_version": pins["node"],
            "python_version": pins["python"],
            "golangci_lint_version": pins["golangci-lint"],
        }
    )
    if selection["go"]:
        outputs["go_groups"] = json.dumps(group_names())
        if selection["integration"]:
            modules = list(integration_test_packages())
            outputs["integration_modules"] = json.dumps(modules)
            outputs["integration_shards"] = json.dumps(integration_module_shards(modules))
    outputs.setdefault("go_groups", "[]")
    outputs.setdefault("integration_modules", "[]")
    outputs.setdefault("integration_shards", "[]")
    return outputs


def main() -> int:
    try:
        outputs = outputs_for(changed_paths(os.environ.get("CHANGE_BASE", "")))
        output_path = os.environ.get("GITHUB_OUTPUT")
        if output_path:
            write_github_output(Path(output_path), outputs)
        for name in ("go", "web", "integration", "stale", "security_go", "security_web"):
            print(f"{name}: {outputs[name]}")
    except (OSError, ValueError, RuntimeError, KeyError) as error:
        print(f"CI change detection failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
