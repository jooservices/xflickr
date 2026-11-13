#!/usr/bin/env python3
"""Tests for classifying changed paths into CI job requirements."""

from __future__ import annotations

import unittest

from ci_changes import classify_paths


class CIChangesTest(unittest.TestCase):
    def test_docs_only_changes_skip_go_web_integration_and_security(self) -> None:
        self.assertEqual(
            classify_paths(["docs/README.md", "docs/architecture.md"]),
            {"go": False, "web": False, "integration": False, "stale": False, "security_go": False, "security_web": False},
        )

    def test_web_only_change_skips_all_go_jobs(self) -> None:
        self.assertEqual(
            classify_paths(["web/src/pages/home.tsx"]),
            {"go": False, "web": True, "integration": False, "stale": False, "security_go": False, "security_web": True},
        )

    def test_backend_contract_and_workflow_changes_select_relevant_jobs(self) -> None:
        backend = classify_paths(["backend/internal/catalog/app/search.go"])
        self.assertEqual(backend, {"go": True, "web": False, "integration": True, "stale": True, "security_go": True, "security_web": False})
        contract = classify_paths(["contracts/openapi/openapi.yaml"])
        self.assertEqual(contract, {"go": True, "web": True, "integration": True, "stale": True, "security_go": True, "security_web": True})


if __name__ == "__main__":
    unittest.main()
