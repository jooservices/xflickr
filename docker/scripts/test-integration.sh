#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
BACKEND="$ROOT/backend"
LOG_DIR="$ROOT/.tmp/integration"

if [[ "${CI:-false}" == "true" ]]; then
	if [[ -e "$ROOT/.env" ]]; then
		printf 'CI integration run refuses an existing .env file\n' >&2
		exit 1
	fi
	umask 077
	{
		printf 'DB_HOST=%s\n' "$DB_HOST"
		printf 'DB_PORT=%s\n' "$DB_PORT"
		printf 'DB_NAME=%s\n' "$DB_NAME"
		printf 'DB_DATABASE=%s\n' "$DB_DATABASE"
		printf 'DB_USER=%s\n' "$DB_USER"
		printf 'DB_USERNAME=%s\n' "$DB_USERNAME"
		printf 'DB_PASSWORD=%s\n' "$DB_PASSWORD"
		printf 'DB_SSLMODE=%s\n' "$DB_SSLMODE"
		printf 'REDIS_HOST=%s\n' "$REDIS_HOST"
		printf 'REDIS_PORT=%s\n' "$REDIS_PORT"
		printf 'REDIS_DB=%s\n' "$REDIS_DB"
		printf 'REDIS_PASSWORD=%s\n' "${REDIS_PASSWORD:-}"
		printf 'REDIS_PREFIX=%s\n' "$REDIS_PREFIX"
		printf 'SEARCH_URL=%s\n' "$SEARCH_URL"
		printf 'SEARCH_USERNAME=%s\n' "$SEARCH_USERNAME"
		printf 'SEARCH_PASSWORD=%s\n' "$SEARCH_PASSWORD"
		printf 'OPENSEARCH_INDEX_PREFIX=%s\n' "$OPENSEARCH_INDEX_PREFIX"
		printf 'MAIL_SMTP_ADDR=%s\n' "$MAIL_SMTP_ADDR"
		printf 'MAIL_FROM=%s\n' "$MAIL_FROM"
		printf 'XFLICKR_MASTER_KEYS=%s\n' "$XFLICKR_MASTER_KEYS"
		printf 'XFLICKR_MASTER_KEY_ACTIVE=%s\n' "$XFLICKR_MASTER_KEY_ACTIVE"
		printf 'NODE_ID=%s\n' "$NODE_ID"
		printf 'XFLICKR_WORKTREE_PATH=%s\n' "$ROOT"
	} >"$ROOT/.env"
	trap 'rm -f "$ROOT/.env"' EXIT
fi

if [[ "${CI:-false}" != "true" && -f "$ROOT/.env" ]]; then
	"$SCRIPT_DIR/dev.sh" verify-db
fi
mkdir -p "$LOG_DIR"

run_module() {
	local module="$1"
	local log="$LOG_DIR/$module.jsonl"
	local test_pattern
	python3 "$SCRIPT_DIR/clean_db.py"
	mapfile -t packages < <(python3 "$SCRIPT_DIR/integration_shards.py" integration-packages "$module")
	if [[ ${#packages[@]} -eq 0 ]]; then
		printf 'Integration module %s has no opted-in packages.\n' "$module" >&2
		exit 1
	fi
	test_pattern="$(python3 "$SCRIPT_DIR/integration_shards.py" integration-test-pattern "$module")"
	printf '\n==> Integration module: %s (%d packages; marked test files only)\n' "$module" "${#packages[@]}"
	(
		cd "$BACKEND"
		XFLICKR_INTEGRATION_TEST=1 \
		XFLICKR_DBTEST_INTEGRATION_TEST=1 \
		RUNTIME_PASS_ENV='XFLICKR_INTEGRATION_TEST XFLICKR_DBTEST_INTEGRATION_TEST' \
		"$SCRIPT_DIR/run.sh" go go test -json -count=1 -p 1 -run "$test_pattern" "${packages[@]}"
	) | tee "$log"
	python3 "$SCRIPT_DIR/integration_test_count.py" "$log"
}

if [[ -n "${TEST_MODULE:-}" && -n "${TEST_MODULES:-}" ]]; then
	printf 'Set either TEST_MODULE or TEST_MODULES, not both.\n' >&2
	exit 1
elif [[ -n "${TEST_MODULE:-}" ]]; then
	run_module "$TEST_MODULE"
elif [[ -n "${TEST_MODULES:-}" ]]; then
	IFS=',' read -r -a requested_modules <<< "$TEST_MODULES"
	group_logs=()
	if [[ ${#requested_modules[@]} -eq 0 ]]; then
		printf 'TEST_MODULES must contain at least one module.\n' >&2
		exit 1
	fi
	for module in "${requested_modules[@]}"; do
		run_module "$module"
		group_logs+=("$LOG_DIR/$module.jsonl")
	done
	python3 "$SCRIPT_DIR/integration_test_count.py" --minimum 0 "${group_logs[@]}"
else
	all_modules="$(python3 "$SCRIPT_DIR/integration_shards.py" integration-modules | python3 -c 'import json,sys; print("\n".join(json.load(sys.stdin)))')"
	while IFS= read -r module; do
		[[ -z "$module" ]] || run_module "$module"
	done <<<"$all_modules"
	python3 "$SCRIPT_DIR/integration_test_count.py" --minimum 100 "$LOG_DIR"
fi
