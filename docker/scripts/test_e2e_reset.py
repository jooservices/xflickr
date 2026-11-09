import os
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).parent))
import e2e_reset  # noqa: E402


class E2EResetTests(unittest.TestCase):
    def test_uses_the_configured_host_endpoint_without_exposing_password(self):
        runner = Mock()

        with patch.dict(os.environ, {"PGPASSWORD": "test-only"}):
            e2e_reset.reset_database(
                "127.0.0.1", "15435", "xflickr", "xflickr_wtjooxf337", runner=runner
            )

        command = runner.call_args.args[0]
        self.assertIn("127.0.0.1", command)
        self.assertIn("15435", command)
        self.assertIn("xflickr_wtjooxf337", command)
        self.assertNotIn("password", " ".join(command).lower())
        self.assertTrue(runner.call_args.kwargs["check"])

    def test_reset_failure_is_propagated(self):
        runner = Mock(side_effect=RuntimeError("psql failed"))

        with patch.dict(os.environ, {"PGPASSWORD": "test-only"}):
            with self.assertRaisesRegex(RuntimeError, "psql failed"):
                e2e_reset.reset_database(
                    "127.0.0.1", "15435", "xflickr", "xflickr_wtjooxf337", runner=runner
                )

        runner.assert_called_once()

    def test_e2e_rechecks_the_same_host_endpoint_before_migration(self):
        script = Path(__file__).with_name("e2e.sh").read_text()
        reset = script.index("e2e_reset.py")
        verified = script.index('"$SCRIPT_DIR/dev.sh" verify-db', reset)
        migrate = script.index('"$STACK_DIR/xflickr" migrate up', verified)

        self.assertLess(reset, verified)
        self.assertLess(verified, migrate)
        self.assertIn('export DB_HOST="$db_host" DB_PORT="$db_port"', script)
        self.assertNotIn("docker exec", script)


if __name__ == "__main__":
    unittest.main()
