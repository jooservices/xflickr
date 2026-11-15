#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
LINTER_VERSION="$(awk '$1 == "golangci-lint" { print $2 }' "$ROOT/.tool-versions")"

if [[ -z "$LINTER_VERSION" ]]; then
	printf 'golangci-lint version is missing from .tool-versions.\n' >&2
	exit 1
fi

# The tests of the Workspaces delivery flow files (profile, hooks, PR guard) need the Workspaces tools around the
# checkout, so they run where a person or an AI develops and not on GitHub (CI=true there).
if [[ "${CI:-}" != "true" ]]; then
	for flow_test in test_ai_profile.py test_ai_hooks.py test_pr_guard.py; do
		"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/$flow_test"
	done
fi

cd "$ROOT/backend"
"$SCRIPT_DIR/run.sh" go go vet ./...
"$SCRIPT_DIR/run.sh" go go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$LINTER_VERSION" \
	run --config "$ROOT/.golangci.yml" ./...
