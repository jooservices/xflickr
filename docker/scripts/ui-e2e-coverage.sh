#!/usr/bin/env bash
# Gate: every catalogue view has a mapped E2E scenario whose file exists.
# Usage: ui-e2e-coverage.sh
# Env overrides (used by the shell tests): CATALOGUE, COVERAGE_DOC, E2E_COVERAGE_ROOT.
# UI_E2E_COVERAGE=warn prints the uncovered list and exits 0; any other value fails.
# Uses POSIX tools only so that it runs on Linux and macOS.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
CATALOGUE="${CATALOGUE:-$ROOT/docs/ui/README.md}"
COVERAGE_DOC="${COVERAGE_DOC:-$ROOT/docs/ui/e2e-coverage.md}"
E2E_COVERAGE_ROOT="${E2E_COVERAGE_ROOT:-$ROOT}"
MODE="${UI_E2E_COVERAGE:-fail}"

covered=0
missing=0
deferred=0
uncovered=""

fail() {
	printf 'ui-e2e-coverage: %s\n' "$*" >&2
}

cell_value() {
	printf '%s' "$1" | tr -d '`' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

is_deferred() {
	case "$1" in
		*"deferred:"*) return 0 ;;
		*) return 1 ;;
	esac
}

is_missing() {
	[[ "$1" == MISSING* || -z "$1" || "$1" == "-" || "$1" == "--" ]]
}

if [[ ! -f "$CATALOGUE" ]]; then
	fail "catalogue not found: $CATALOGUE"
	exit 1
fi
if [[ ! -f "$COVERAGE_DOC" ]]; then
	fail "coverage document not found: $COVERAGE_DOC"
	exit 1
fi

while IFS= read -r line; do
	case "$line" in
		"| \`"*) ;;
		*) continue ;;
	esac
	id="$(printf '%s' "$line" | awk -F'|' '{print $2}' | tr -d '` ')"
	row="$(grep -F "| \`$id\` |" "$COVERAGE_DOC" | head -n 1 || true)"
	if [[ -z "$row" ]]; then
		missing=$((missing + 1))
		uncovered="$uncovered$id "
		fail "$id: no row in the coverage document"
		continue
	fi
	file_cell="$(cell_value "$(printf '%s' "$row" | awk -F'|' '{print $3}')")"
	if is_missing "$file_cell"; then
		if is_deferred "$row"; then
			deferred=$((deferred + 1))
			continue
		fi
		missing=$((missing + 1))
		uncovered="$uncovered$id "
		fail "$id: scenario is MISSING"
		continue
	fi
	if [[ ! -f "$E2E_COVERAGE_ROOT/$file_cell" ]]; then
		missing=$((missing + 1))
		uncovered="$uncovered$id "
		fail "$id: mapped scenario file is missing: $file_cell"
		continue
	fi
	covered=$((covered + 1))
done <"$CATALOGUE"

total=$((covered + missing + deferred))
printf 'ui-e2e-coverage: %d covered, %d missing, %d deferred of %d catalogue views\n' \
	"$covered" "$missing" "$deferred" "$total"
if [[ -n "$uncovered" ]]; then
	printf '%s\n' "$uncovered" | tr ' ' '\n' | while IFS= read -r slug; do
		[[ -n "$slug" ]] || continue
		if [[ "$MODE" == "warn" ]]; then
			printf '%s\n' "ui-e2e-coverage: WARNING: uncovered: $slug"
		else
			printf '%s\n' "ui-e2e-coverage: uncovered: $slug"
		fi
	done
fi
if ((missing > 0)) && [[ "$MODE" != "warn" ]]; then
	printf 'ui-e2e-coverage: FAILED with %d uncovered view(s)\n' "$missing" >&2
	exit 1
fi
