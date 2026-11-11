#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"
cd "$repo_root/backend"
XFLICKR_INTEGRATION_TEST=1 XFLICKR_INCIDENT_PLAN_TEST=1 \
	go test -p 1 ./internal/ops/adapters/postgres -run '^TestOpenIncidentsPlan$' -count=1 -v
