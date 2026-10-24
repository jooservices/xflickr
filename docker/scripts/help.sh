#!/usr/bin/env bash
set -euo pipefail

cat <<'EOF'
XFlickr developer commands:
  make help             Show this command list.
  make lint             Run ShellCheck and Go vet.
  make arch-lint        Load the Go package graph for architecture checks.
  make test             Run unit tests and the coverage gate.
  make test-integration Run tests against the configured worktree database.
  make e2e              Run the web end-to-end suite when it is configured.
  make gen              Run Go code generators.
  make check            Run all release verification steps.
  make dev-up           Check shared service health and worktree databases.
  make dev-down         Stop XFlickr-owned development services, if any.
EOF
