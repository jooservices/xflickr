#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
LINTER_VERSION="$(awk '$1 == "golangci-lint" { print $2 }' "$ROOT/.tool-versions")"

if [[ -z "$LINTER_VERSION" ]]; then
	printf 'golangci-lint version is missing from .tool-versions.\n' >&2
	exit 1
fi

cd "$ROOT/backend"
"$SCRIPT_DIR/run.sh" go go vet ./...
"$SCRIPT_DIR/run.sh" go go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$LINTER_VERSION" \
	run --config "$ROOT/.golangci.yml" ./...
