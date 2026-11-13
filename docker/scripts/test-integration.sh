#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
BACKEND="$ROOT/backend"
LOG_DIR="$ROOT/.tmp/integration"

if [[ -f "$ROOT/.env" ]]; then
	"$SCRIPT_DIR/dev.sh" verify-db
fi
mkdir -p "$LOG_DIR"

run_module() {
	local module="$1"
	local log="$LOG_DIR/$module.jsonl"
	python3 "$SCRIPT_DIR/clean_db.py"
	mapfile -t packages < <(python3 "$SCRIPT_DIR/integration_shards.py" integration-packages "$module")
	if [[ ${#packages[@]} -eq 0 ]]; then
		printf 'Integration module %s has no opted-in packages.\n' "$module" >&2
		exit 1
	fi
	printf '\n==> Integration module: %s (%d packages)\n' "$module" "${#packages[@]}"
	(
		cd "$BACKEND"
		XFLICKR_INTEGRATION_TEST=1 \
		XFLICKR_DBTEST_INTEGRATION_TEST=1 \
		RUNTIME_PASS_ENV='XFLICKR_INTEGRATION_TEST XFLICKR_DBTEST_INTEGRATION_TEST' \
		"$SCRIPT_DIR/run.sh" go go test -json -count=1 -p 1 "${packages[@]}"
	) | tee "$log"
	python3 "$SCRIPT_DIR/integration_test_count.py" "$log"
}

if [[ -n "${TEST_MODULE:-}" ]]; then
	run_module "$TEST_MODULE"
else
	modules="$(python3 "$SCRIPT_DIR/integration_shards.py" integration-modules | python3 -c 'import json,sys; print("\n".join(json.load(sys.stdin)))')"
	while IFS= read -r module; do
		[[ -z "$module" ]] || run_module "$module"
	done <<<"$modules"
	python3 "$SCRIPT_DIR/integration_test_count.py" --minimum 100 "$LOG_DIR"
fi
