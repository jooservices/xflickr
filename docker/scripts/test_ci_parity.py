#!/usr/bin/env python3
"""Tests for the CI and local verification parity check."""

from __future__ import annotations

import unittest

from ci_parity import WORKFLOW, check_parity, workflow_commands, workflow_configuration


class CIParityTest(unittest.TestCase):
    def test_workflow_command_missing_from_verify_names_the_command(self) -> None:
        with self.assertRaisesRegex(ValueError, "build-web"):
            check_parity(
                "jobs:\n  web:\n    steps:\n      - run: make build-web\n",
                ["static"],
                {"github_only": [], "local_only": []},
            )

    def test_workflow_shell_command_outside_make_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "python3"):
            check_parity(
                "jobs:\n  web:\n    steps:\n      - run: python3 check.py\n",
                ["static"],
                {"github_only": [], "local_only": []},
            )

    def test_explicit_github_and_local_layers_are_allowed(self) -> None:
        check_parity(
            "jobs:\n  web:\n    steps:\n      - run: make static\n"
            "      - run: make security-audit\n",
            ["static", "e2e"],
            {"github_only": ["security-audit"], "local_only": ["e2e"]},
        )

    def test_workflow_targets_are_collected_from_inline_and_block_runs(self) -> None:
        workflow = """jobs:
  static:
    steps:
      - run: make static
      - run: |
          make test-shard GROUP=core
          make test-shard GROUP=web
"""
        self.assertEqual(
            workflow_commands(workflow), {"static", "test-shard"}
        )

    def test_workflow_requires_scoped_push_and_ref_concurrency(self) -> None:
        workflow = """on:
  push:
    branches: [develop, master]
  pull_request:
concurrency:
  group: ci-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true
"""
        workflow_configuration(workflow)

    def test_workflow_rejects_unfiltered_push(self) -> None:
        with self.assertRaisesRegex(ValueError, "push branches"):
            workflow_configuration(
                "on:\n  push:\n  pull_request:\n"
                "concurrency:\n  group: ci-${{ github.ref }}\n"
                "  cancel-in-progress: true\n"
            )

    def test_repository_workflow_has_the_required_triggers_and_concurrency(self) -> None:
        workflow_configuration(WORKFLOW.read_text(encoding="utf-8"))

    def test_failure_cancellation_steps_run_after_failed_steps(self) -> None:
        lines = WORKFLOW.read_text(encoding="utf-8").splitlines()
        cancel_steps = [
            index for index, line in enumerate(lines)
            if "uses: ./.github/actions/cancel-on-failure" in line
        ]
        self.assertGreater(len(cancel_steps), 0)
        for index in cancel_steps:
            with self.subTest(line=index + 1):
                self.assertEqual(lines[index - 1].strip(), "- if: ${{ failure() }}")


if __name__ == "__main__":
    unittest.main()
