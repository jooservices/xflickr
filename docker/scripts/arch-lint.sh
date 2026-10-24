#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go list ./...
)

printf 'Architecture package loading passed; explicit boundary rules are not configured yet.\n'
