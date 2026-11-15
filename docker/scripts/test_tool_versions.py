#!/usr/bin/env python3
"""Tests for the runtime version pin and warning helper."""

from __future__ import annotations

import unittest

from check_tool_versions import mismatches, parse_versions


class ToolVersionsTest(unittest.TestCase):
    def test_reads_all_supported_tool_pins(self) -> None:
        versions = parse_versions(
            "go 1.27.4\nnodejs 24\ngolangci-lint 2.15.0\npython 3.12\n"
        )
        self.assertEqual(versions["go"], "1.27.4")
        self.assertEqual(versions["node"], "24")
        self.assertEqual(versions["golangci-lint"], "2.15.0")
        self.assertEqual(versions["python"], "3.12")

    def test_warns_with_pinned_and_installed_node_and_go_versions(self) -> None:
        warnings = mismatches(
            {"go": "1.27.4", "node": "24", "python": "3.13"},
            {"go": "go1.27.4 linux/amd64", "node": "v26.13.0", "python": "3.13.18"},
        )
        self.assertEqual(len(warnings), 1)
        self.assertIn("Node", warnings[0])
        self.assertIn("24", warnings[0])
        self.assertIn("26.13.0", warnings[0])

    def test_accepts_patch_versions_within_a_major_minor_pin(self) -> None:
        self.assertEqual(
            mismatches(
                {"go": "1.27.4", "node": "24", "python": "3.12"},
                {"go": "go1.27.4 linux/amd64", "node": "v24.13.1", "python": "3.12.8"},
            ),
            [],
        )

    def test_rejects_duplicate_and_unknown_tool_entries(self) -> None:
        with self.assertRaisesRegex(ValueError, "duplicate"):
            parse_versions("go 1.27.4\ngo 1.27.4\n")
        with self.assertRaisesRegex(ValueError, "unsupported"):
            parse_versions("ruby 3.4.1\n")


if __name__ == "__main__":
    unittest.main()
