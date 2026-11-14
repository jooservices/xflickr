#!/usr/bin/env python3
"""Tests for XFlickr's workspace AI profile (.ai/project.json): every stage points at a command that exists in this repository."""

from __future__ import annotations

import json
import re
import shlex
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKSPACE_TOKEN = "{workspace}"
STAGE_KEYS = {"name", "group", "order", "cmd", "cwd", "when", "parallel", "expectedSeconds", "requiresServices"}
REQUIRED = {"unit-go", "unit-web", "integration", "e2e", "lint-go", "lint-web", "e2e-layers", "scripts-web", "architecture", "complexity", "secrets", "security-go", "security-web", "sonar"}


def profile() -> dict:
    return json.loads((ROOT / ".ai/project.json").read_text(encoding="utf-8"))


def make_targets() -> set[str]:
    return set(re.findall(r"^([A-Za-z0-9_.-]+):(?:\s|$)", (ROOT / "Makefile").read_text(encoding="utf-8"), re.MULTILINE))


class AIProfileTests(unittest.TestCase):
    def test_the_profile_declares_the_checks_of_the_project(self) -> None:
        names = {stage["name"] for stage in profile()["stages"]}
        self.assertTrue(REQUIRED <= names, REQUIRED - names)
        self.assertEqual(len(names), len(profile()["stages"]), "stage names must be unique")

    def test_every_stage_has_the_required_fields_and_an_existing_directory(self) -> None:
        orders = []
        for stage in profile()["stages"]:
            with self.subTest(stage=stage["name"]):
                self.assertTrue(STAGE_KEYS <= set(stage) <= STAGE_KEYS | {"changedCmd"})
                self.assertTrue((ROOT / stage["cwd"]).is_dir())
                self.assertIn(stage["group"], {"static", "test", "analysis"})
                orders.append(stage["order"])
        self.assertEqual(sorted(orders), orders)

    def test_a_quick_command_that_takes_files_says_which_files_cover_it(self) -> None:
        for stage in profile()["stages"]:
            if "{files}" in stage.get("changedCmd", ""):
                with self.subTest(stage=stage["name"]):
                    self.assertTrue(stage["when"])

    def test_every_command_runs_something_that_exists(self) -> None:
        targets = make_targets()
        for stage in profile()["stages"]:
            for field in ("cmd", "changedCmd"):
                if field not in stage:
                    continue
                with self.subTest(stage=stage["name"], field=field):
                    words = shlex.split(stage[field])
                    cwd = ROOT / stage["cwd"]
                    if words[0] == "make":
                        self.assertIn(words[1], targets)
                    elif words[0] in {"bash", "python3"}:
                        self.assertTrue((cwd / words[1]).is_file(), words[1])
                    elif words[0].startswith(WORKSPACE_TOKEN):
                        self.assertTrue(words[0].endswith("/scripts/dev/sonar"))

    def test_the_setup_steps_prepare_a_fresh_worktree(self) -> None:
        steps = profile()["setup"]
        self.assertEqual(["env", "web-deps", "go-deps"], [step["name"] for step in steps])
        for step in steps:
            with self.subTest(step=step["name"]):
                self.assertTrue((ROOT / step["cwd"]).is_dir())
        self.assertIn("{ticket_id}", steps[0]["cmd"])
        self.assertTrue((ROOT / shlex.split(steps[0]["cmd"])[1]).is_file())

    def test_the_project_commits_as_one_identity_and_forbids_ai_traces(self) -> None:
        identity = profile()["identity"]
        self.assertEqual({"name": "Viet Vu", "email": "jooservices@gmail.com"}, {key: identity[key] for key in ("name", "email")})
        self.assertTrue(identity["forbiddenTraces"])
        for pattern in identity["forbiddenTraces"]:
            re.compile(pattern)

    def test_the_pull_request_template_and_its_guard_are_in_place(self) -> None:
        template = (ROOT / ".github/pull_request_template.md").read_text(encoding="utf-8")
        headings = [line[3:] for line in template.splitlines() if line.startswith("## ")]
        self.assertEqual(["Jira", "Requirement", "What", "Why", "Out of scope", "Risk / decisions", "Tests", "Verification"], headings)
        workflow = (ROOT / ".github/workflows/pr-guard.yml").read_text(encoding="utf-8")
        self.assertIn(".github/pr_guard.py check", workflow)
        self.assertTrue((ROOT / ".github/pr_guard.py").is_file())

    def test_the_policy_fields_are_present(self) -> None:
        for field in ("testGlobs", "commitMessage", "protectedBranches", "ticketKeyRegex", "forbiddenPatterns", "mechanicalPaths", "skills", "touchPoints"):
            self.assertIn(field, profile())
        self.assertEqual(["develop", "main"], profile()["protectedBranches"])


if __name__ == "__main__":
    unittest.main()
