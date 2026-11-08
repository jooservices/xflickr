#!/usr/bin/env python3
"""Check the Go version pin shared by the module, CI and Docker build."""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VERSION = r"\d+\.\d+\.\d+"


def module_go_version(text: str) -> str:
    """Return the module's exact Go version directive."""
    matches = re.findall(r"(?m)^go\s+(" + VERSION + r")\s*$", text)
    if len(matches) != 1:
        raise ValueError("backend/go.mod must contain one exact Go version")
    return matches[0]


def docker_go_version(text: str) -> str:
    """Return the Go version used by the Docker build stage."""
    arg_matches = re.findall(r"(?m)^ARG GO_VERSION=(" + VERSION + r")\s*$", text)
    if len(arg_matches) != 1:
        raise ValueError("Dockerfile must define one exact GO_VERSION argument")

    images = re.findall(r"(?m)^FROM\s+golang:([^\s]+)", text)
    if not images:
        raise ValueError("Dockerfile must have a golang build image")

    versions: list[str] = []
    for image in images:
        if image == "${GO_VERSION}-alpine":
            versions.append(arg_matches[0])
            continue

        match = re.fullmatch(r"(" + VERSION + r")(?:-[\w.]+)?", image)
        if match is None:
            raise ValueError(f"unsupported golang Docker image tag: {image}")
        versions.append(match[1])

    if len(set(versions)) != 1:
        raise ValueError("Dockerfile uses different Go image versions")
    return versions[0]


def ci_go_versions(text: str, module_version: str) -> list[str]:
    """Return Go versions configured by setup-go and direct Go image tags."""
    lines = text.splitlines()
    versions: list[str] = []
    for index, line in enumerate(lines):
        if re.search(r"\buses:\s*actions/setup-go@", line) is None:
            continue

        step_indent = None
        for previous in range(index, -1, -1):
            match = re.match(r"^(\s*)-\s+", lines[previous])
            if match is not None:
                step_indent = len(match[1])
                break

        if step_indent is None:
            raise ValueError("could not determine an actions/setup-go step")

        end = len(lines)
        for following in range(index + 1, len(lines)):
            match = re.match(r"^(\s*)-\s+", lines[following])
            if match is not None and len(match[1]) == step_indent:
                end = following
                break

        block = "\n".join(lines[index:end])
        file_matches = re.findall(
            r"(?m)^\s+go-version-file:\s*([^\s#]+)", block
        )
        version_matches = re.findall(r"(?m)^\s+go-version:\s*([^\s#]+)", block)
        if len(file_matches) + len(version_matches) != 1:
            raise ValueError("each actions/setup-go step must pin one Go version")

        if file_matches:
            if file_matches[0] != "backend/go.mod":
                raise ValueError("CI must read its Go version from backend/go.mod")
            versions.append(module_version)
        else:
            version = version_matches[0]
            if re.fullmatch(VERSION, version) is None:
                raise ValueError(f"unsupported CI Go version: {version}")
            versions.append(version)

    for image in re.findall(r"\bgolang:([^\s]+)", text):
        match = re.fullmatch(r"(" + VERSION + r")(?:-[\w.]+)?", image)
        if match is None:
            raise ValueError(f"unsupported CI Go image tag: {image}")
        versions.append(match[1])

    return versions


def check_versions(
    module_text: str,
    dockerfile_text: str,
    workflow_texts: list[str],
) -> str:
    """Raise a useful error when Go versions differ across build inputs."""
    module_version = module_go_version(module_text)
    docker_version = docker_go_version(dockerfile_text)
    ci_versions: list[str] = []
    for workflow in workflow_texts:
        ci_versions.extend(ci_go_versions(workflow, module_version))

    mismatches = [
        f"Dockerfile uses {docker_version} (backend/go.mod uses {module_version})"
    ] if docker_version != module_version else []
    mismatches.extend(
        f"CI uses {version} (backend/go.mod uses {module_version})"
        for version in ci_versions
        if version != module_version
    )
    if not ci_versions:
        raise ValueError("no CI Go version pin was found")
    if mismatches:
        raise ValueError("; ".join(mismatches))
    return module_version


def main() -> int:
    """Check repository version pins and report the result."""
    workflows = sorted((ROOT / ".github/workflows").glob("*.yml"))
    workflows.extend(sorted((ROOT / ".github/workflows").glob("*.yaml")))
    try:
        version = check_versions(
            (ROOT / "backend/go.mod").read_text(encoding="utf-8"),
            (ROOT / "Dockerfile").read_text(encoding="utf-8"),
            [workflow.read_text(encoding="utf-8") for workflow in workflows],
        )
    except (OSError, ValueError) as error:
        print(f"Go version check failed: {error}", file=sys.stderr)
        return 1

    print(f"Go version check passed: {version} (go.mod, CI, Dockerfile)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
