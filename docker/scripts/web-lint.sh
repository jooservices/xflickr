#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

cd "$ROOT/web"
if [[ ! -d node_modules ]]; then
	"$SCRIPT_DIR/run.sh" node npm ci
fi
"$SCRIPT_DIR/run.sh" node npm run lint
"$SCRIPT_DIR/run.sh" node npx tsc --noEmit
"$SCRIPT_DIR/web-lint-changed.sh"
