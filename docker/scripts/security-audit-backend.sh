#!/usr/bin/env bash
# Scans the backend module and its dependencies for known vulnerabilities that the code can reach.
# The tool version is pinned so a new govulncheck release cannot change the CI result by itself.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
cd -- "$ROOT/backend"
exec go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
