#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

cd "$ROOT/web"
if [[ ! -d node_modules ]]; then
	"$SCRIPT_DIR/run.sh" node npm ci
fi
if [[ -n "${WEB_SHARD:-}" ]]; then
	"$SCRIPT_DIR/run.sh" node npx vitest run --shard="$WEB_SHARD" --coverage.enabled=false --reporter=default \
		--reporter=junit --outputFile.junit=vitest-report.xml
else
	"$SCRIPT_DIR/run.sh" node npm run test
fi
