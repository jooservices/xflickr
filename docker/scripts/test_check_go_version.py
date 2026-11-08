"""Tests for the Go version consistency check."""

from __future__ import annotations

import unittest

from check_go_version import check_versions, docker_go_version, module_go_version


GO_MOD = "module example.com/app\n\ngo 1.27.3\n"
DOCKERFILE = "ARG GO_VERSION=1.27.3\nFROM golang:${GO_VERSION}-alpine AS build\n"
CI = """jobs:
  build:
    steps:
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
"""


class GoVersionTest(unittest.TestCase):
    """Cover accepted pins and mismatches in each build input."""

    def test_module_requires_one_exact_version(self) -> None:
        with self.assertRaisesRegex(ValueError, "one exact Go version"):
            module_go_version("module example.com/app\ngo 1.27\n")

    def test_docker_image_uses_its_declared_version(self) -> None:
        self.assertEqual(docker_go_version(DOCKERFILE), "1.27.3")
        with self.assertRaisesRegex(ValueError, "different Go image versions"):
            docker_go_version(
                "ARG GO_VERSION=1.27.3\n"
                "FROM golang:${GO_VERSION}-alpine AS build\n"
                "FROM golang:1.26.0-alpine AS other\n"
            )

    def test_ci_uses_the_module_version(self) -> None:
        self.assertEqual(check_versions(GO_MOD, DOCKERFILE, [CI]), "1.27.3")

    def test_ci_literal_must_match_the_module(self) -> None:
        pinned_ci = CI.replace("go-version-file: backend/go.mod", "go-version: 1.26.0")
        with self.assertRaisesRegex(ValueError, "CI uses 1.26.0"):
            check_versions(GO_MOD, DOCKERFILE, [pinned_ci])

    def test_ci_version_file_must_be_the_module(self) -> None:
        pinned_ci = CI.replace("backend/go.mod", ".go-version")
        with self.assertRaisesRegex(ValueError, "must read its Go version"):
            check_versions(GO_MOD, DOCKERFILE, [pinned_ci])


if __name__ == "__main__":
    unittest.main()
