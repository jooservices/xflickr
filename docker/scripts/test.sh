#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
TEMP_DIR="$ROOT/.tmp"

"$SCRIPT_DIR/ui-spec-check.test.sh"

mkdir -p "$TEMP_DIR"
coverage_file="$(mktemp "$TEMP_DIR/xflickr-coverage.XXXXXX")"
trap 'rm -f "$coverage_file"' EXIT

(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go test -count=1 -coverprofile="$coverage_file" ./...
)

"$SCRIPT_DIR/coverage-gate.sh" "$coverage_file"

if [[ -f "$ROOT/web/package.json" ]]; then
	(
		cd "$ROOT/web"
		"$SCRIPT_DIR/run.sh" node npm run test
	)
fi
