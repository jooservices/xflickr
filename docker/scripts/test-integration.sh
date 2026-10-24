#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

"$SCRIPT_DIR/dev.sh" verify-db
(
	cd "$ROOT/backend"
	XFLICKR_INTEGRATION_TEST=1 \
	XFLICKR_DBTEST_INTEGRATION_TEST=1 \
	RUNTIME_PASS_ENV='XFLICKR_INTEGRATION_TEST XFLICKR_DBTEST_INTEGRATION_TEST' \
	"$SCRIPT_DIR/run.sh" go go test -count=1 ./...
)
