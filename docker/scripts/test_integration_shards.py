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

    def test_integration_modules_are_assigned_to_one_of_six_job_shards(self) -> None:
        modules = list(integration_test_packages())
        shards = integration_module_shards(modules)
        assigned = [module for shard in shards for module in shard["modules"].split(",")]

        self.assertEqual(len(shards), 6)
        self.assertEqual(sorted(assigned), sorted(modules))
        self.assertEqual(len(assigned), len(set(assigned)))

    def test_unknown_integration_module_requires_a_shard_assignment(self) -> None:
        with self.assertRaisesRegex(ValueError, "missing shard assignment: future-module"):
            integration_module_shards(["future-module"])

    def test_integration_test_pattern_selects_tests_in_marked_files(self) -> None:
        with TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            marked = root / "internal/accounts/repository_integration_test.go"
            marked.parent.mkdir(parents=True)
            marked.write_text(
                "// XFLICKR_INTEGRATION_TEST\n"
                "func TestRepositoryIntegration(t *testing.T) {}\n"
                "func TestSharedFixture(t *testing.T) {}\n",
                encoding="utf-8",
            )
            unmarked = root / "internal/accounts/unit_test.go"
            unmarked.write_text("func TestUnitOnly(t *testing.T) {}\n", encoding="utf-8")

            tests = integration_test_names(root)

        self.assertEqual(
            tests,
            {"accounts": ["TestRepositoryIntegration", "TestSharedFixture"]},
        )
        self.assertEqual(
            integration_test_pattern("accounts", tests),
            "^(TestRepositoryIntegration|TestSharedFixture)$",
        )

    def test_integration_test_pattern_rejects_empty_module(self) -> None:
        with self.assertRaisesRegex(ValueError, "has no test functions"):
            integration_test_pattern("accounts", {})


if __name__ == "__main__":
    unittest.main()
