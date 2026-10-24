#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

run_step() {
	local step="$1"
	printf '\n==> %s\n' "$step"
	if "$SCRIPT_DIR/$step.sh"; then
		return 0
	else
		local status=$?
		printf 'FAIL: %s (exit %s)\n' "$step" "$status" >&2
		exit "$status"
	fi
}

run_make_target() {
	local step="$1"
	printf '\n==> %s\n' "$step"
	if make -C "$ROOT" --no-print-directory "$step"; then
		return 0
	else
		local status=$?
		printf 'FAIL: %s (exit %s)\n' "$step" "$status" >&2
		exit "$status"
	fi
}

run_step lint
run_step arch-lint
run_make_target ui-spec-check
run_step test
run_step test-integration
run_step e2e
run_step gen

printf '\nAll release verification steps passed.\n'
