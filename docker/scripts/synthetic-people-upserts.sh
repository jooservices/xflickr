#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

python3 "$ROOT/docker/scripts/worktree_env.py" --check
(cd "$ROOT/backend" && XFLICKR_INTEGRATION_TEST=1 XFLICKR_PEOPLE_CONTENTION=1 \
	go test -p 1 ./internal/catalog/adapters/postgres \
	-run '^TestSyntheticPeopleContention$' -count=1 -v)
