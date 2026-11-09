#!/usr/bin/env bash
# Gate: every catalogue view has a mapped E2E scenario whose file exists.
# Usage: ui-e2e-coverage.sh
# Env overrides (used by the shell tests): CATALOGUE, COVERAGE_DOC,
# E2E_COVERAGE_ROOT, E2E_COVERAGE_BASELINE.
# UI_E2E_COVERAGE=warn prints the uncovered list and exits 0; any other value fails.
# Missing views listed in the baseline are known gaps and do not fail;
# new missing views, baseline views that became covered, and stale
# baseline entries fail (the baseline can only shrink).
# Uses POSIX tools only so that it runs on Linux and macOS.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
CATALOGUE="${CATALOGUE:-$ROOT/docs/ui/README.md}"
COVERAGE_DOC="${COVERAGE_DOC:-$ROOT/docs/ui/e2e-coverage.md}"
E2E_COVERAGE_ROOT="${E2E_COVERAGE_ROOT:-$ROOT}"
BASELINE="${E2E_COVERAGE_BASELINE:-$ROOT/docs/ui/e2e-coverage-baseline.txt}"
MODE="${UI_E2E_COVERAGE:-fail}"

covered=0
missing=0
deferred=0
known_gaps=0
new_missing=0
now_covered=0
stale=0
uncovered=""
known=""
catalogue_slugs=""
missing_slugs=""
baseline_slugs=""

fail() {
	printf 'ui-e2e-coverage: %s\n' "$*" >&2
}

contains() {
	case " $1 " in
		*" $2 "*) return 0 ;;
		*) return 1 ;;
	esac
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

record_missing() {
	missing=$((missing + 1))
	missing_slugs="$missing_slugs $1"
	if contains "$baseline_slugs" "$1"; then
		known_gaps=$((known_gaps + 1))
		known="$known $1"
	else
		new_missing=$((new_missing + 1))
		uncovered="$uncovered $1"
		fail "$1: $2"
	fi
}

if [[ ! -f "$CATALOGUE" ]]; then
	fail "catalogue not found: $CATALOGUE"
	exit 1
fi
if [[ ! -f "$COVERAGE_DOC" ]]; then
	fail "coverage document not found: $COVERAGE_DOC"
	exit 1
fi

if [[ -f "$BASELINE" ]]; then
	while IFS= read -r line || [[ -n "$line" ]]; do
		slug="$(printf '%s' "$line" | sed -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
		[[ -n "$slug" ]] || continue
		if ! contains "$baseline_slugs" "$slug"; then
			baseline_slugs="$baseline_slugs $slug"
		fi
	done <"$BASELINE"
fi

while IFS= read -r line; do
	case "$line" in
		"| \`"*) ;;
		*) continue ;;
	esac
	id="$(printf '%s' "$line" | awk -F'|' '{print $2}' | tr -d '` ')"
	catalogue_slugs="$catalogue_slugs $id"
	row="$(grep -F "| \`$id\` |" "$COVERAGE_DOC" | head -n 1 || true)"
	if [[ -z "$row" ]]; then
		record_missing "$id" "no row in the coverage document"
		continue
	fi
	file_cell="$(cell_value "$(printf '%s' "$row" | awk -F'|' '{print $3}')")"
	if is_missing "$file_cell"; then
		if is_deferred "$row"; then
			deferred=$((deferred + 1))
			continue
		fi
		record_missing "$id" "scenario is MISSING"
		continue
	fi
	if [[ ! -f "$E2E_COVERAGE_ROOT/$file_cell" ]]; then
		record_missing "$id" "mapped scenario file is missing: $file_cell"
		continue
	fi
	covered=$((covered + 1))
done <"$CATALOGUE"

for slug in $baseline_slugs; do
	if ! contains "$catalogue_slugs" "$slug"; then
		stale=$((stale + 1))
		fail "stale baseline entry: $slug (not in catalogue; delete its baseline line)"
	elif ! contains "$missing_slugs" "$slug"; then
		now_covered=$((now_covered + 1))
		fail "Baseline view now covered: $slug (delete its baseline line)"
	fi
done

total=$((covered + missing + deferred))
printf 'ui-e2e-coverage: %d covered, %d missing (%d known gaps, %d new), %d deferred of %d catalogue views\n' \
	"$covered" "$missing" "$known_gaps" "$new_missing" "$deferred" "$total"
if [[ -n "$known" ]]; then
	printf '%s\n' "$known" | tr ' ' '\n' | while IFS= read -r slug; do
		[[ -n "$slug" ]] || continue
		printf '%s\n' "ui-e2e-coverage: known gap: $slug"
	done
fi
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
failures=$((new_missing + now_covered + stale))
if ((failures > 0)) && [[ "$MODE" != "warn" ]]; then
	printf 'ui-e2e-coverage: FAILED with %d uncovered view(s)\n' "$failures" >&2
	exit 1
fi
