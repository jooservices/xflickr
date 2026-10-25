#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

"$SCRIPT_DIR/run.sh" python python3 "$ROOT/docker/scripts/api-usage-check.py" --root "${API_USAGE_ROOT:-$ROOT}"
