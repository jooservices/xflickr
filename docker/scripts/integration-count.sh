#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

python3 "$SCRIPT_DIR/integration_test_count.py" --minimum 100 "$ROOT/.tmp/integration"
