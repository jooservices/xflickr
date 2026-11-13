#!/usr/bin/env python3
"""Tests for classifying changed paths into CI job requirements."""

from __future__ import annotations

import json
import unittest

from ci_changes import classify_paths, outputs_for


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

    def test_workflow_changes_select_integration_and_stale_checks(self) -> None:
        workflow = classify_paths([".github/workflows/ci.yml"])

        self.assertTrue(workflow["integration"])
        self.assertTrue(workflow["stale"])

    def test_generator_changes_select_stale_check(self) -> None:
        self.assertTrue(classify_paths(["docker/scripts/gen.sh"])["stale"])
        self.assertTrue(classify_paths(["docker/scripts/gen-stale.sh"])["stale"])
        self.assertTrue(classify_paths(["web/orval.config.ts"])["stale"])

    def test_architecture_configuration_change_selects_go_checks(self) -> None:
        self.assertTrue(classify_paths([".go-arch-lint.yml"])["go"])

    def test_backend_change_publishes_complete_integration_shards(self) -> None:
        outputs = outputs_for(["backend/internal/cluster/app/assignment.go"])
        shards = json.loads(outputs["integration_shards"])
        modules = [module for shard in shards for module in shard["modules"].split(",")]

        self.assertEqual(len(shards), 7)
        self.assertEqual(len(modules), len(set(modules)))
        self.assertEqual(
            set(modules),
            {
                "accounts", "ai", "catalog", "cluster", "cmd", "crawl", "home",
                "identity", "media", "ops", "platform", "quota", "schedule", "search",
                "settings", "sharing", "sync",
            },
        )
        self.assertEqual(
            {shard["name"] for shard in shards if shard["elasticsearch"]},
            {"search"},
        )


if __name__ == "__main__":
    unittest.main()
