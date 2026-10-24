#!/usr/bin/env bash
set -euo pipefail

cat <<'EOF'
XFlickr developer commands:
  make help             Show this command list.
  make lint             Run ShellCheck, Go vet, and web lint when the web app is present.
  make arch-lint        Load the Go package graph for architecture checks.
  make test             Run Go and web unit tests, then the Go coverage gate.
  make test-integration Run tests against the configured worktree database.
  make e2e              Run the Playwright smoke suite when the web app is present.
  make gen              Bundle the OpenAPI contract and generate the Go server and TypeScript client.
  make gen-stale        Fail when make gen has changed any file.
  make ui-spec-check    Verify POC tokens, root media overrides, and icons.
  make check            Run all release verification steps.
  make dev-up           Check shared service health and worktree databases.
  make dev-down         Stop XFlickr-owned development services, if any.
EOF
