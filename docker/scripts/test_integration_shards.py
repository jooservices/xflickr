#!/usr/bin/env python3
"""Tests for Go package and integration module partitioning."""

from __future__ import annotations

import unittest

from integration_shards import group_packages, integration_modules


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


if __name__ == "__main__":
    unittest.main()
