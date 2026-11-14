#!/usr/bin/env python3
"""Unit tests for XFlickr's worktree environment mapping and guard."""

from __future__ import annotations

import io
import json
import os
import stat
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

from worktree_env import build_env, check_env, database_name, parse_env_text, render_env, run, validate_worktree_id

ROOT = Path(__file__).resolve().parents[2]
WORKTREE_ID = "jooxf68"
RESOURCE = "wt_jooxf68"
EXAMPLE = {
    "DB_NAME": "xflickr_test",
    "REDIS_DB": "8",
    "REDIS_USERNAME": "",
    "HTTP_PORT": "8080",
    "XFLICKR_MASTER_KEYS": "",
}
TREE = {
    "TREE_NAME": WORKTREE_ID,
    "TREE_POSTGRES_HOST": "127.0.0.1",
    "TREE_POSTGRES_PORT": "15435",
    "TREE_POSTGRES_DATABASE": RESOURCE,
    "TREE_POSTGRES_USER": RESOURCE,
    "TREE_POSTGRES_PASSWORD": "tree-postgres-secret",
    "TREE_REDIS_HOST": "127.0.0.1",
    "TREE_REDIS_PORT": "16382",
    "TREE_REDIS_USER": RESOURCE,
    "TREE_REDIS_PASSWORD": "tree-redis-secret",
    "TREE_REDIS_PREFIX": f"{RESOURCE}:",
    "TREE_ELASTICSEARCH_HOST": "127.0.0.1",
    "TREE_ELASTICSEARCH_PORT": "19200",
    "TREE_ELASTICSEARCH_INDEX_PREFIX": f"{RESOURCE}-",
    "TREE_ELASTICSEARCH_USER": RESOURCE,
    "TREE_ELASTICSEARCH_PASSWORD": "tree-search-secret",
}


class FakeDatabase:
    """Records which isolated database the guard tries to reach."""

    def __init__(self, available=()):
        self.available = set(available)
        self.checked = []

    def check(self, name):
        self.checked.append(name)
        if name not in self.available:
            raise RuntimeError("database unavailable")


class EnvTests(unittest.TestCase):
    def test_parse_handles_quotes_comments_and_special_characters(self):
        text = "# comment\n\nA=plain\nB='quoted value'\nC=\"double\"\nD=[=W3/niqng?._0K\nbroken line\n"
        self.assertEqual(
            parse_env_text(text),
            {"A": "plain", "B": "quoted value", "C": "double", "D": "[=W3/niqng?._0K"},
        )

    def test_render_round_trips_values(self):
        values = {"A": "1", "B": "two words"}
        self.assertEqual(parse_env_text(render_env(values)), values)

    def test_render_keeps_dollar_quote_and_escape_characters_literal(self):
        values = {"A": "pa$$word", "B": "a$WORD", "C": "it's", "D": "it's $x", "E": "back\\slash", "F": "multi\nline", "G": "${HOME}x"}
        rendered = render_env(values)
        self.assertEqual(parse_env_text(rendered), values)
        self.assertIn("A='pa$$word'", rendered)


class ValidationTests(unittest.TestCase):
    def test_accepts_ticket_ids(self):
        for value in ("jooxf68", "a", "x" * 24):
            self.assertEqual(validate_worktree_id(value), value)

    def test_rejects_unsafe_ticket_ids(self):
        for value in ("", "UP", "has space", "a-b", "x" * 25, "../x", "a;drop"):
            with self.assertRaises(ValueError, msg=value):
                validate_worktree_id(value)

    def test_AC1_database_name_uses_the_db_tree_resource_name(self):
        self.assertEqual(database_name(WORKTREE_ID), RESOURCE)
        self.assertNotIn(database_name(WORKTREE_ID), {"xflickr", "xflickr_test"})


class MappingTests(unittest.TestCase):
    def test_AC1_maps_tree_credentials_and_preserves_other_environment_values(self):
        existing = {"HTTP_PORT": "9090", "CUSTOM_SETTING": "kept", "XFLICKR_MASTER_KEYS": "k9:existing"}
        with patch.dict(os.environ, {"DOCKER_POSTGRES_USER": "must-not-be-used"}):
            env = build_env(EXAMPLE, TREE, WORKTREE_ID, existing, "k1:newkey", "/w/xflickr")

        self.assertEqual(
            {key: env[key] for key in ("DB_HOST", "DB_PORT", "DB_NAME", "DB_DATABASE", "DB_USER", "DB_USERNAME", "DB_PASSWORD")},
            {
                "DB_HOST": TREE["TREE_POSTGRES_HOST"],
                "DB_PORT": TREE["TREE_POSTGRES_PORT"],
                "DB_NAME": TREE["TREE_POSTGRES_DATABASE"],
                "DB_DATABASE": TREE["TREE_POSTGRES_DATABASE"],
                "DB_USER": TREE["TREE_POSTGRES_USER"],
                "DB_USERNAME": TREE["TREE_POSTGRES_USER"],
                "DB_PASSWORD": TREE["TREE_POSTGRES_PASSWORD"],
            },
        )
        self.assertEqual(
            {key: env[key] for key in ("REDIS_HOST", "REDIS_PORT", "REDIS_USERNAME", "REDIS_PASSWORD", "REDIS_PREFIX")},
            {
                "REDIS_HOST": TREE["TREE_REDIS_HOST"],
                "REDIS_PORT": TREE["TREE_REDIS_PORT"],
                "REDIS_USERNAME": TREE["TREE_REDIS_USER"],
                "REDIS_PASSWORD": TREE["TREE_REDIS_PASSWORD"],
                "REDIS_PREFIX": TREE["TREE_REDIS_PREFIX"],
            },
        )
        self.assertEqual(
            {key: env[key] for key in ("SEARCH_URL", "SEARCH_USERNAME", "SEARCH_PASSWORD", "OPENSEARCH_INDEX_PREFIX")},
            {
                "SEARCH_URL": "http://127.0.0.1:19200",
                "SEARCH_USERNAME": TREE["TREE_ELASTICSEARCH_USER"],
                "SEARCH_PASSWORD": TREE["TREE_ELASTICSEARCH_PASSWORD"],
                "OPENSEARCH_INDEX_PREFIX": TREE["TREE_ELASTICSEARCH_INDEX_PREFIX"],
            },
        )
        self.assertEqual(env["REDIS_DB"], EXAMPLE["REDIS_DB"])
        self.assertEqual(env["XFLICKR_WORKTREE_PATH"], "/w/xflickr")
        self.assertEqual(env["HTTP_PORT"], "9090")
        self.assertEqual(env["CUSTOM_SETTING"], "kept")
        self.assertEqual(env["XFLICKR_MASTER_KEYS"], "k9:existing")
        self.assertNotIn("must-not-be-used", render_env(env))

    def test_preserves_master_key_and_creates_it_when_missing(self):
        env = build_env(EXAMPLE, TREE, WORKTREE_ID, {}, "k1:newkey")
        self.assertEqual(env["XFLICKR_MASTER_KEYS"], "k1:newkey")
        self.assertEqual(env["XFLICKR_MASTER_KEY_ACTIVE"], "k1")
        kept = build_env(EXAMPLE, TREE, WORKTREE_ID, {"XFLICKR_MASTER_KEYS": "k9:old", "XFLICKR_MASTER_KEY_ACTIVE": "k9"}, "k1:newkey")
        self.assertEqual(kept["XFLICKR_MASTER_KEYS"], "k9:old")
        self.assertEqual(kept["XFLICKR_MASTER_KEY_ACTIVE"], "k9")

    def test_refuses_tree_resources_for_another_ticket(self):
        other_tree = {**TREE, "TREE_NAME": "jooxf99", "TREE_POSTGRES_DATABASE": "wt_jooxf99"}
        with self.assertRaises(ValueError):
            build_env(EXAMPLE, other_tree, WORKTREE_ID, {}, "k1:newkey")

    def test_reports_missing_tree_values_by_key(self):
        with self.assertRaises(KeyError):
            build_env(EXAMPLE, {"TREE_NAME": WORKTREE_ID}, WORKTREE_ID, {}, "k1:newkey")


class RunTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        (self.root / ".env.example").write_text("DB_NAME=xflickr_test\nREDIS_DB=8\nXFLICKR_MASTER_KEYS=\n")
        (self.root / ".env.tree").write_text(render_env(TREE))

    def tearDown(self):
        self.temporary.cleanup()

    def call(self, *args):
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = run(["--worktree", str(self.root), *args])
        return code, stdout.getvalue() + stderr.getvalue()

    def test_AC1_writes_from_env_tree_without_reading_shared_docker_credentials(self):
        (self.root / ".env.docker").unlink(missing_ok=True)
        (self.root / ".env").write_text(render_env({"CUSTOM_SETTING": "kept", "XFLICKR_MASTER_KEYS": "k9:existing"}))
        with patch.dict(os.environ, {"DOCKER_POSTGRES_USER": "must-not-be-used", "DOCKER_POSTGRES_PASSWORD": "must-not-be-used"}):
            code, output = self.call(WORKTREE_ID)
        self.assertEqual(code, 0, output)
        self.assertTrue((self.root / ".env").is_file())
        self.assertEqual(stat.S_IMODE((self.root / ".env").stat().st_mode), 0o600)
        written = parse_env_text((self.root / ".env").read_text())
        expected = {
            "DB_HOST": TREE["TREE_POSTGRES_HOST"],
            "DB_PORT": TREE["TREE_POSTGRES_PORT"],
            "DB_NAME": TREE["TREE_POSTGRES_DATABASE"],
            "DB_DATABASE": TREE["TREE_POSTGRES_DATABASE"],
            "DB_USER": TREE["TREE_POSTGRES_USER"],
            "DB_USERNAME": TREE["TREE_POSTGRES_USER"],
            "DB_PASSWORD": TREE["TREE_POSTGRES_PASSWORD"],
            "REDIS_HOST": TREE["TREE_REDIS_HOST"],
            "REDIS_PORT": TREE["TREE_REDIS_PORT"],
            "REDIS_USERNAME": TREE["TREE_REDIS_USER"],
            "REDIS_PASSWORD": TREE["TREE_REDIS_PASSWORD"],
            "REDIS_PREFIX": TREE["TREE_REDIS_PREFIX"],
            "SEARCH_URL": "http://127.0.0.1:19200",
            "SEARCH_USERNAME": TREE["TREE_ELASTICSEARCH_USER"],
            "SEARCH_PASSWORD": TREE["TREE_ELASTICSEARCH_PASSWORD"],
            "OPENSEARCH_INDEX_PREFIX": TREE["TREE_ELASTICSEARCH_INDEX_PREFIX"],
        }
        self.assertEqual({key: written[key] for key in expected}, expected)
        self.assertEqual(written["CUSTOM_SETTING"], "kept")
        self.assertEqual(written["XFLICKR_MASTER_KEYS"], "k9:existing")
        self.assertNotIn("must-not-be-used", (self.root / ".env").read_text())
        self.assertNotIn("DOCKER_POSTGRES", output)

    def test_AC2_missing_env_tree_fails_without_writing_env(self):
        (self.root / ".env.tree").unlink()
        code, output = self.call(WORKTREE_ID)
        self.assertNotEqual(code, 0)
        self.assertIn("scripts/db/tree create", output)
        self.assertFalse((self.root / ".env").exists())

    def test_rejects_an_invalid_ticket_id_without_touching_env(self):
        code, _ = self.call("Bad Id")
        self.assertNotEqual(code, 0)
        self.assertFalse((self.root / ".env").exists())

    def test_rejects_a_tree_file_owned_by_another_ticket_without_touching_env(self):
        (self.root / ".env.tree").write_text(render_env({**TREE, "TREE_NAME": "jooxf99"}))
        code, _ = self.call(WORKTREE_ID)
        self.assertNotEqual(code, 0)
        self.assertFalse((self.root / ".env").exists())


class CheckTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.root.joinpath(".env.tree").write_text(render_env(TREE))
        self.database = FakeDatabase({RESOURCE})
        self.reachable = lambda host, port: True
        self.write_env()

    def tearDown(self):
        self.temporary.cleanup()

    def write_env(self, db=RESOURCE, alias=None, user=RESOURCE, username=RESOURCE, path=None):
        values = build_env(EXAMPLE, TREE, WORKTREE_ID, {}, "k1:testkey", str(path or self.root.resolve()))
        values["DB_NAME"] = db
        values["DB_DATABASE"] = alias or db
        values["DB_USER"] = user
        values["DB_USERNAME"] = username
        self.root.joinpath(".env").write_text(render_env(values))

    def test_AC3_accepts_only_the_matching_tree_database_and_user(self):
        self.assertEqual(check_env(self.root, self.database, self.reachable), [])
        self.assertEqual(self.database.checked, [RESOURCE])

    def test_AC3_refuses_live_test_root_and_different_tree_databases(self):
        cases = (
            ("xflickr", "xflickr", RESOURCE, RESOURCE, "live or shared test database"),
            ("xflickr_test", "xflickr_test", RESOURCE, RESOURCE, "live or shared test database"),
            (RESOURCE, RESOURCE, "postgres", "postgres", "root account"),
            ("wt_jooxf99", "wt_jooxf99", RESOURCE, RESOURCE, ".env.tree"),
        )
        for db, alias, user, username, reason in cases:
            with self.subTest(db=db, user=user):
                self.write_env(db, alias, user, username)
                problems = check_env(self.root, self.database, self.reachable)
                self.assertTrue(problems)
                self.assertIn(reason, problems[0])

    def test_refuses_environment_copied_from_another_worktree(self):
        self.write_env(path="/somewhere/else")
        self.assertIn("another worktree", check_env(self.root, self.database, self.reachable)[0])

    def test_refuses_aliases_that_select_different_databases(self):
        self.write_env(alias="wt_jooxf99")
        self.assertIn("database keys", check_env(self.root, self.database, self.reachable)[0])

    def test_requires_env_tree(self):
        self.root.joinpath(".env.tree").unlink()
        self.assertIn(".env.tree", check_env(self.root, self.database, self.reachable)[0])

    def test_reports_unavailable_database_and_services(self):
        problems = check_env(self.root, FakeDatabase(), lambda host, port: False)
        self.assertEqual(len(problems), 3)


class AcceptanceCriteriaTests(unittest.TestCase):
    def test_AC4_profile_setup_leaves_database_environment_creation_to_the_developer(self):
        profile = json.loads((ROOT / ".ai" / "project.json").read_text())
        setup = profile["setup"]
        self.assertEqual([step["name"] for step in setup], ["web-deps", "go-deps"])
        self.assertFalse(any("worktree_env.py" in step["cmd"] for step in setup))
        self.assertFalse(any("scripts/db/tree" in step["cmd"] for step in setup))

    def test_AC6_environment_guide_documents_setup_commands_and_workspace_guide(self):
        guide = (ROOT / "docs" / "ENVIRONMENT.md").read_text()
        self.assertIn("scripts/db/tree create", guide)
        self.assertIn("python3 docker/scripts/worktree_env.py", guide)
        self.assertIn("guides/worktree-databases.md", guide)
        self.assertIn("scripts/db/tree drop <ticket-id>", guide)


if __name__ == "__main__":
    unittest.main()
