#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

if [[ -f "$ROOT/web/package.json" ]]; then
	(
		cd "$ROOT/web"
		"$SCRIPT_DIR/run.sh" node npm run e2e
	)
else
	printf 'No web end-to-end suite is configured yet.\n'
fi
