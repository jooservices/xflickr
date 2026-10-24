#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

"$SCRIPT_DIR/run.sh" shellcheck shellcheck "$SCRIPT_DIR"/*.sh
(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go vet ./...
)
