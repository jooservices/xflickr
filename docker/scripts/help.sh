#!/usr/bin/env bash
set -euo pipefail

cat <<'EOF'
XFlickr developer commands:
  make help             Show this command list.
  make static           Run fast parity, migration, complexity and architecture checks.
  make verify           Run CI stages locally, stopping at the first failure.
  make lint             Run Go vet and golangci-lint.
  make web-lint         Run web formatting, TypeScript and ESLint checks.
  make web-test         Run web unit tests and coverage.
  make go-test          Run Go unit-test shards.
  make go-coverage      Merge Go coverage shards and enforce coverage gates.
  make arch-lint        Load the Go package graph for architecture checks.
  make test             Run Go and web unit tests, then the Go coverage gate.
  make test-integration Recreate the worktree database and run integration tests by module.
  make clean-db         Recreate only this worktree database and apply migrations.
  make e2e              Run the Playwright smoke suite when the web app is present.
  make gen              Bundle OpenAPI, run sqlc, and generate the Go server and TypeScript client.
  make gen-stale        Fail when make gen has changed any file.
  make ui-spec-check    Verify POC tokens, icons, component state tables, and component markdown.
  make check            Alias for make verify.
  make check-tool-versions Warn when Go, Node or Python differs from .tool-versions.
  make precheck         Fail on duplicate or malformed migration versions.
  make next-migration NAME=x  Print a new migration filename above every known version.
  make dev-up           Check shared service health and worktree databases.
  make dev-down         Stop XFlickr-owned development services, if any.
  make develop-up       Build and start the XFlickr develop application container.
  make develop-down     Stop the XFlickr develop container and retain its data volume.
  make develop-logs     Follow logs from the XFlickr develop container.
  make develop-status   Show the XFlickr develop container status.
EOF
