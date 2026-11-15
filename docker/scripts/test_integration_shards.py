#!/usr/bin/env python3
"""Tests for Go package and integration module partitioning."""

from __future__ import annotations

import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from integration_shards import (
    group_packages,
    integration_modules,
    integration_module_shards,
    integration_test_names,
    integration_test_packages,
    integration_test_pattern,
    verify_integration_test_coverage,
)


class IntegrationShardsTest(unittest.TestCase):
    def test_go_packages_are_assigned_once_to_a_package_group(self) -> None:
        packages = [
            "github.com/jooservices/xflickr/internal/accounts/app",
            "github.com/jooservices/xflickr/internal/catalog/app",
            "github.com/jooservices/xflickr/internal/media/app",
            "github.com/jooservices/xflickr/internal/schedule/app",
            "github.com/jooservices/xflickr/internal/platform/db",
        ]
        groups = group_packages(packages)
        self.assertEqual(sum(map(len, groups.values())), len(packages))
        self.assertEqual(len({package for shard in groups.values() for package in shard}), len(packages))

    def test_unknown_package_is_rejected_instead_of_omitted(self) -> None:
        with self.assertRaisesRegex(ValueError, "no Go test group"):
            group_packages(["github.com/jooservices/xflickr/tools/unknown"])

    def test_integration_packages_are_sharded_by_feature_module(self) -> None:
        package_tests = {
            "github.com/jooservices/xflickr/internal/accounts": "TestAccount\nXFLICKR_INTEGRATION_TEST",
            "github.com/jooservices/xflickr/internal/platform/mail/adapters/smtp": "XFLICKR_INTEGRATION_TEST",
            "github.com/jooservices/xflickr/internal/search/adapters/elasticsearch": "XFLICKR_INTEGRATION_TEST",
            "github.com/jooservices/xflickr/internal/catalog/app": "func TestUnitOnly()",
        }
        shards = integration_modules(package_tests)
        self.assertEqual(set(shards), {"accounts", "platform", "search"})
        self.assertEqual(shards["platform"], [
            "github.com/jooservices/xflickr/internal/platform/mail/adapters/smtp"
        ])
        self.assertNotIn("catalog", shards)

    def test_integration_modules_are_assigned_to_one_of_seven_job_shards(self) -> None:
        modules = list(integration_test_packages())
        shards = integration_module_shards(modules)
        assigned = [module for shard in shards for module in shard["modules"].split(",")]

        self.assertEqual(len(shards), 7)
        self.assertEqual(sorted(assigned), sorted(modules))
        self.assertEqual(len(assigned), len(set(assigned)))
        self.assertEqual(
            {shard["name"] for shard in shards if shard["elasticsearch"]},
            {"search"},
        )

    def test_unknown_integration_module_requires_a_shard_assignment(self) -> None:
        with self.assertRaisesRegex(ValueError, "missing shard assignment: future-module"):
            integration_module_shards(["future-module"])

    def test_integration_test_pattern_selects_package_tests_with_shared_helpers(self) -> None:
        with TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            package_dir = root / "internal/crawl/adapters/postgres"
            package_dir.mkdir(parents=True)
            marked_helper = package_dir / "engine_integration_test.go"
            marked_helper.write_text(
                "// XFLICKR_INTEGRATION_TEST\n"
                "func openIntegrationPool(t *testing.T) {}\n"
                "func TestEngineIntegration(t *testing.T) {}\n",
                encoding="utf-8",
            )
            contacts = package_dir / "contacts_integration_test.go"
            contacts.write_text(
                "func TestContactsRefreshRemovesMissingRowsOnlyAfterFinalPage(t *testing.T) { openIntegrationPool(t) }\n"
                "func TestContactsRefreshFailureRetainsExistingRows(t *testing.T) { openIntegrationPool(t) }\n",
                encoding="utf-8",
            )
            unrelated = root / "internal/accounts/unit_test.go"
            unrelated.parent.mkdir(parents=True)
            unrelated.write_text("func TestUnitOnly(t *testing.T) {}\n", encoding="utf-8")

            packages = integration_test_packages(root)
            tests = integration_test_names(root)

        self.assertEqual(
            tests,
            {
                "crawl": [
                    "TestContactsRefreshFailureRetainsExistingRows",
                    "TestContactsRefreshRemovesMissingRowsOnlyAfterFinalPage",
                    "TestEngineIntegration",
                ]
            },
        )
        self.assertEqual(
            packages,
            {"crawl": ["github.com/jooservices/xflickr/internal/crawl/adapters/postgres"]},
        )
        pattern = integration_test_pattern("crawl", tests)
        self.assertRegex("TestContactsRefreshRemovesMissingRowsOnlyAfterFinalPage", pattern)
        self.assertNotIn("accounts", tests)

    def test_repository_contacts_tests_using_shared_integration_fixture_are_discovered(self) -> None:
        tests = integration_test_names()

        self.assertIn("TestContactsRefreshRemovesMissingRowsOnlyAfterFinalPage", tests["crawl"])
        self.assertIn("TestContactsRefreshFailureRetainsExistingRows", tests["crawl"])
        self.assertIn("TestContactsCrawlWithoutPreInsertedAccountPerson", tests["crawl"])

    def test_outbox_integration_tests_are_enabled_by_local_and_ci_runners(self) -> None:
        root = Path(__file__).resolve().parents[2]
        runner = (root / "docker/scripts/test-integration.sh").read_text(encoding="utf-8")
        workflow = (root / ".github/workflows/ci.yml").read_text(encoding="utf-8")

        self.assertIn("XFLICKR_OUTBOX_INTEGRATION_TEST=1", runner)
        self.assertIn("XFLICKR_OUTBOX_INTEGRATION_TEST", workflow)

    def test_integration_coverage_guard_rejects_tests_without_a_shard(self) -> None:
        with TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            package_dir = root / "internal/future"
            package_dir.mkdir(parents=True)
            (package_dir / "shared_integration_test.go").write_text(
                "// XFLICKR_INTEGRATION_TEST\nfunc TestSharedFixture(t *testing.T) {}\n",
                encoding="utf-8",
            )
            with self.assertRaisesRegex(ValueError, "missing shard assignment: future"):
                # The repository shard map has no future module, so it must not silently omit it.
                verify_integration_test_coverage(root)

    def test_integration_test_pattern_rejects_empty_module(self) -> None:
        with self.assertRaisesRegex(ValueError, "has no test functions"):
            integration_test_pattern("accounts", {})


if __name__ == "__main__":
    unittest.main()
