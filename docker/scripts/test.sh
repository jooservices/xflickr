#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/test_coverage_gate.py"
"$SCRIPT_DIR/ui-spec-check.test.sh"

"$SCRIPT_DIR/coverage-gate.sh"

if [[ -f "$ROOT/web/package.json" ]]; then
	(
		cd "$ROOT/web"
		"$SCRIPT_DIR/run.sh" node npm run test
	)
fi
