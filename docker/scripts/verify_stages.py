"""Canonical local verification stages mirrored by the CI workflow."""

VERIFY_STAGES = (
    "static",
    "lint",
    "web-lint",
    "web-test",
    "go-test",
    "go-coverage",
    "test-integration",
    "gen-stale",
)
