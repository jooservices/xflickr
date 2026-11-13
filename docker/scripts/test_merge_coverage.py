#!/usr/bin/env python3
"""Tests for combining disjoint Go coverage shards."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from merge_coverage import merge_profiles


class MergeCoverageTest(unittest.TestCase):
    def test_merges_profile_blocks_under_one_header(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = root / "first.out"
            second = root / "second.out"
            merged = root / "merged.out"
            first.write_text("mode: set\na.go:1.1,2.1 1 1\n", encoding="utf-8")
            second.write_text("mode: set\nb.go:1.1,2.1 1 0\n", encoding="utf-8")

            merge_profiles([first, second], merged)

            self.assertEqual(
                merged.read_text(encoding="utf-8"),
                "mode: set\na.go:1.1,2.1 1 1\nb.go:1.1,2.1 1 0\n",
            )

    def test_rejects_empty_and_mixed_mode_profiles(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            empty = root / "empty.out"
            mixed = root / "mixed.out"
            output = root / "merged.out"
            empty.write_text("", encoding="utf-8")
            mixed.write_text("mode: count\nb.go:1.1,2.1 1 0\n", encoding="utf-8")
            good = root / "good.out"
            good.write_text("mode: set\na.go:1.1,2.1 1 1\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "coverage profile is empty"):
                merge_profiles([empty], output)
            with self.assertRaisesRegex(ValueError, "coverage modes differ"):
                merge_profiles([good, mixed], output)


if __name__ == "__main__":
    unittest.main()
