#!/usr/bin/env python3
"""Unit tests for the worktree environment helper."""

import io
import os
import stat
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from worktree_env import (
    Provisioner,
    check_env,
    build_env,
    database_name,
    parse_env_text,
    render_env,
    run,
    validate_worktree_id,
)

EXAMPLE = {"DB_NAME": "xflickr_test", "REDIS_DB": "8", "HTTP_PORT": "8080", "XFLICKR_MASTER_KEYS": ""}
SHARED = {
    "DOCKER_POSTGRES_HOST": "127.0.0.1",
    "DOCKER_POSTGRES_PORT": "15435",
    "DOCKER_POSTGRES_USER": "root",
    "DOCKER_POSTGRES_PASSWORD": "db-secret",
    "DOCKER_REDIS_HOST": "127.0.0.1",
    "DOCKER_REDIS_PORT": "16382",
    "DOCKER_REDIS_PASSWORD": "redis-secret",
    "DOCKER_ELASTIC_HOST": "127.0.0.1",
    "DOCKER_ELASTIC_PORT": "19200",
    "DOCKER_ELASTIC_USER": "elastic",
    "DOCKER_ELASTIC_PASSWORD": "es-secret",
}


class FakeAdmin:
    """Records database creation instead of talking to PostgreSQL."""

    def __init__(self, existing=()):
        self.databases = set(existing)
        self.created = []

    def exists(self, name):
        return name in self.databases

    def create(self, name):
        self.databases.add(name)
        self.created.append(name)


class ParseTests(unittest.TestCase):
    def test_parse_handles_quotes_comments_and_special_characters(self):
        text = "# comment\n\nA=plain\nB='quoted value'\nC=\"double\"\nD=[=W3/niqng?._0K\nbroken line\n"
        self.assertEqual(
            parse_env_text(text),
            {"A": "plain", "B": "quoted value", "C": "double", "D": "[=W3/niqng?._0K"},
        )

    def test_render_round_trips_values(self):
        values = {"A": "1", "B": "two words"}
        self.assertEqual(parse_env_text(render_env(values)), values)


class ValidationTests(unittest.TestCase):
    def test_accepts_short_lowercase_ids(self):
        for value in ("jooxf68", "a", "x" * 24):
            self.assertEqual(validate_worktree_id(value), value)

    def test_rejects_unsafe_ids(self):
        for value in ("", "UP", "has space", "a-b", "x" * 25, "../x", "a;drop"):
            with self.assertRaises(ValueError, msg=value):
                validate_worktree_id(value)

    def test_database_name_is_never_live_or_test(self):
        self.assertEqual(database_name("jooxf68"), "xflickr_wtjooxf68")
        self.assertNotIn(database_name("jooxf68"), {"xflickr", "xflickr_test"})


class BuildEnvTests(unittest.TestCase):
    def build(self, existing=None):
        return build_env(EXAMPLE, SHARED, "jooxf68", existing or {}, master_key="k1:newkey")

    def test_selects_worktree_resources(self):
        env = self.build()
        self.assertEqual(env["DB_NAME"], "xflickr_wtjooxf68")
        self.assertEqual(env["DB_USER"], "root")
        self.assertEqual(env["DB_PASSWORD"], "db-secret")
        self.assertEqual(env["REDIS_DB"], "8")
        self.assertEqual(env["REDIS_PREFIX"], "xflickr_wtjooxf68:")
        self.assertEqual(env["OPENSEARCH_INDEX_PREFIX"], "xflickr_wtjooxf68_")
        self.assertEqual(env["SEARCH_URL"], "http://127.0.0.1:19200")
        self.assertEqual(env["NODE_ID"], "xflickr-wtjooxf68")

    def test_generates_a_master_key_only_when_missing(self):
        self.assertEqual(self.build()["XFLICKR_MASTER_KEYS"], "k1:newkey")
        self.assertEqual(self.build()["XFLICKR_MASTER_KEY_ACTIVE"], "k1")
        kept = self.build({"XFLICKR_MASTER_KEYS": "k9:old", "XFLICKR_MASTER_KEY_ACTIVE": "k9"})
        self.assertEqual(kept["XFLICKR_MASTER_KEYS"], "k9:old")
        self.assertEqual(kept["XFLICKR_MASTER_KEY_ACTIVE"], "k9")

    def test_missing_shared_credential_is_reported(self):
        with self.assertRaises(KeyError):
            build_env(EXAMPLE, {}, "jooxf68", {}, master_key="k1:x")


class ProvisionerTests(unittest.TestCase):
    def test_creates_missing_database_once(self):
        admin = FakeAdmin()
        provisioner = Provisioner(admin)
        self.assertTrue(provisioner.ensure_database("jooxf68"))
        self.assertFalse(provisioner.ensure_database("jooxf68"))
        self.assertEqual(admin.created, ["xflickr_wtjooxf68"])


class RunTests(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        (self.root / ".env.example").write_text("DB_NAME=xflickr_test\nREDIS_DB=8\nXFLICKR_MASTER_KEYS=\n")
        shared = "\n".join(f"{k}={v}" for k, v in SHARED.items()) + "\n"
        (self.root / ".env.docker").write_text(shared)

    def tearDown(self):
        self.dir.cleanup()

    def call(self, admin, *args):
        output = io.StringIO()
        with redirect_stdout(output):
            code = run(
                ["--worktree", str(self.root), "--shared-env", str(self.root / ".env.docker"), *args],
                admin=admin,
            )
        return code, output.getvalue()

    def test_writes_private_env_and_never_prints_secrets(self):
        admin = FakeAdmin()
        code, output = self.call(admin, "jooxf68")
        self.assertEqual(code, 0)
        env_path = self.root / ".env"
        self.assertEqual(stat.S_IMODE(env_path.stat().st_mode), 0o600)
        written = parse_env_text(env_path.read_text())
        self.assertEqual(written["DB_NAME"], "xflickr_wtjooxf68")
        self.assertEqual(admin.created, ["xflickr_wtjooxf68"])
        for secret in ("db-secret", "redis-secret", "es-secret", written["XFLICKR_MASTER_KEYS"]):
            self.assertNotIn(secret, output)

    def test_second_run_keeps_the_master_key(self):
        admin = FakeAdmin()
        self.call(admin, "jooxf68")
        first = parse_env_text((self.root / ".env").read_text())["XFLICKR_MASTER_KEYS"]
        self.call(admin, "jooxf68")
        second = parse_env_text((self.root / ".env").read_text())["XFLICKR_MASTER_KEYS"]
        self.assertEqual(first, second)
        self.assertEqual(admin.created, ["xflickr_wtjooxf68"])

    def test_rejects_an_invalid_id_without_touching_anything(self):
        admin = FakeAdmin()
        code, _ = self.call(admin, "Bad Id")
        self.assertEqual(code, 2)
        self.assertEqual(admin.created, [])
        self.assertFalse((self.root / ".env").exists())

    def test_missing_shared_file_fails_cleanly(self):
        (self.root / ".env.docker").unlink()
        code, _ = self.call(FakeAdmin(), "jooxf68")
        self.assertEqual(code, 1)


class CheckTests(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        self.up = lambda host, port: True

    def tearDown(self):
        self.dir.cleanup()

    def write_env(self, db="xflickr_wtjooxf68"):
        (self.root / ".env").write_text(
            f"DB_NAME={db}\nDB_HOST=h\nDB_PORT=1\nDB_USER=u\nDB_PASSWORD=p\n"
            "REDIS_HOST=127.0.0.1\nREDIS_PORT=6379\nSEARCH_URL=http://127.0.0.1:9200\n"
        )

    def test_ready_environment_has_no_problems(self):
        self.write_env()
        self.assertEqual(check_env(self.root, FakeAdmin({"xflickr_wtjooxf68"}), self.up), [])

    def test_missing_env_file(self):
        self.assertEqual(len(check_env(self.root, FakeAdmin(), self.up)), 1)

    def test_live_and_test_databases_are_refused(self):
        for name in ("xflickr", "xflickr_test"):
            self.write_env(name)
            self.assertIn("worktree database", check_env(self.root, FakeAdmin({name}), self.up)[0])

    def test_missing_database_and_unreachable_services_are_reported(self):
        self.write_env()
        problems = check_env(self.root, FakeAdmin(), lambda host, port: False)
        self.assertEqual(len(problems), 3)

    def test_run_check_maps_problems_to_exit_code(self):
        self.write_env()
        output = io.StringIO()
        with redirect_stdout(output):
            code = run(["--check", "--worktree", str(self.root)], FakeAdmin({"xflickr_wtjooxf68"}), self.up)
            failing = run(["--check", "--worktree", str(self.root)], FakeAdmin(), self.up)
        self.assertEqual((code, failing), (0, 1))
        self.assertIn("ready", output.getvalue())


if __name__ == "__main__":
    os.environ.pop("PGPASSWORD", None)
    unittest.main()
