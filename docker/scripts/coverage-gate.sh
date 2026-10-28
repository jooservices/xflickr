#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

if [[ $# -gt 1 ]]; then
	printf 'Usage: %s [coverage-profile]\n' "$0" >&2
	exit 2
fi

if [[ $# -eq 1 ]]; then
	if [[ ! -s "$1" ]]; then
		printf 'A non-empty Go coverage profile is required.\n' >&2
		exit 2
	fi
	"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/coverage_gate.py" \
		"$1" --exclude "$SCRIPT_DIR/coverage-exclude.txt"
	exit $?
fi

TEMP_DIR="$ROOT/.tmp"
mkdir -p "$TEMP_DIR"
coverage_file="$(mktemp "$TEMP_DIR/xflickr-coverage.XXXXXX")"
trap 'rm -f "$coverage_file"' EXIT

(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go test -count=1 -coverprofile="$coverage_file" ./...
)

"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/coverage_gate.py" \
	"$coverage_file" --exclude "$SCRIPT_DIR/coverage-exclude.txt"
