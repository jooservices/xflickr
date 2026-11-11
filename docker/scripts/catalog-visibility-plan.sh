#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"

# This manual-only fixture measures the contacts photo-count query on 400,000
# hidden photos. It is deliberately absent from the normal CI test path.
make -C "$ROOT" dev-up

cd "$ROOT/backend"
XFLICKR_INTEGRATION_TEST=1 XFLICKR_PLAN_FIXTURE=1 \
	go test -p 1 ./internal/catalog/adapters/postgres \
	-run '^TestCatalogReadsApplyTwoUserVisibilityPolicy$' -count=1 -v
