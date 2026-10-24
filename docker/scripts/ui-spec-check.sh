#!/usr/bin/env bash
# Gate: every view in the catalogue has a spec, a mockup and the required states.
# Usage: ui-spec-check.sh [--strict] [slug]
# UI_DIR overrides the docs/ui directory (used by the shell tests).
# Uses POSIX tools only so that it runs on Linux and macOS.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
UI_DIR="${UI_DIR:-$ROOT/docs/ui}"
CATALOGUE="$UI_DIR/README.md"

SPEC_STATES="Loading Empty Error Ready"
MOCKUP_STATES="loading empty error ready"

strict=0
slug=""
for arg in "$@"; do
	case "$arg" in
		--strict) strict=1 ;;
		-*)
			printf 'Usage: %s [--strict] [slug]\n' "$0" >&2
			exit 2
			;;
		*) slug="$arg" ;;
	esac
done
# Checking one slug explicitly means that view must be complete.
[[ -n "$slug" ]] && strict=1

errors=0
pending=0
checked=0
fail() {
	printf 'ui-spec-check: %s\n' "$*" >&2
	errors=$((errors + 1))
}

if [[ ! -f "$CATALOGUE" ]]; then
	fail "catalogue not found: $CATALOGUE"
	exit 1
fi

# Prints the link target of a markdown cell, or the cell text when it is not a link.
cell_target() {
	local cell="$1"
	cell="$(printf '%s' "$cell" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
	case "$cell" in
		*"]("*) printf '%s' "$cell" | sed -e 's/^[^(]*(//' -e 's/)[^)]*$//' ;;
		*) printf '%s' "$cell" ;;
	esac
}

check_spec() {
	local id="$1" file="$2" heading state
	for heading in Purpose Layout Data Interactions States Accessibility "Test hooks"; do
		grep -Eq "^##[[:space:]]+${heading}[[:space:]]*$" "$file" ||
			fail "$id: spec $file lacks the heading '## $heading'"
	done
	for state in $SPEC_STATES; do
		grep -Eq "^###[[:space:]]+${state}[[:space:]]*$" "$file" ||
			fail "$id: spec $file lacks the state '### $state'"
	done
	grep -q 'data-testid' "$file" || fail "$id: spec $file has no data-testid test hook"
}

check_mockup() {
	local id="$1" file="$2" state
	for state in $MOCKUP_STATES; do
		grep -Eq "data-state=[\"']${state}[\"']" "$file" ||
			fail "$id: mockup $file lacks the state section data-state=\"$state\""
	done
}

# resolve_cell <id> <kind> <cell> <check function> <conventional path>
resolve_cell() {
	local id="$1" kind="$2" cell="$3" fn="$4" conventional="$5" target path
	target="$(cell_target "$cell")"
	if [[ "$target" == "pending" ]]; then
		if [[ -f "$UI_DIR/$conventional" ]]; then
			fail "$id: $kind is marked pending but $conventional exists"
			return 0
		fi
		if ((strict)); then
			fail "$id: $kind is pending"
		else
			pending=$((pending + 1))
		fi
		return 0
	fi
	if [[ -z "$target" || "$target" == "-" || "$target" == "—" ]]; then
		fail "$id: catalogue row lacks its $kind"
		return 0
	fi
	path="$UI_DIR/$target"
	if [[ ! -f "$path" ]]; then
		fail "$id: $kind file is missing: $target"
		return 0
	fi
	"$fn" "$id" "$path"
}

listed=" "
found_row=0
while IFS= read -r line; do
	case "$line" in
		"| \`"*) ;;
		*) continue ;;
	esac
	# Fields: 1 id, 2 role, 3 route, 4 title, 5 spec, 6 mockup, 7 ticket, 8 e2e.
	id="$(printf '%s' "$line" | awk -F'|' '{print $2}' | tr -d '` ')"
	spec_cell="$(printf '%s' "$line" | awk -F'|' '{print $6}')"
	mock_cell="$(printf '%s' "$line" | awk -F'|' '{print $7}')"
	listed="$listed$id "
	[[ -n "$slug" && "$id" != "$slug" ]] && continue
	found_row=1
	checked=$((checked + 1))
	resolve_cell "$id" spec "$spec_cell" check_spec "views/$id.md"
	resolve_cell "$id" mockup "$mock_cell" check_mockup "mockups/views/$id.html"
done <"$CATALOGUE"

if [[ -n "$slug" && $found_row -eq 0 ]]; then
	fail "$slug: not in the catalogue"
fi

# A spec file that is not in the catalogue is an orphan.
if [[ -d "$UI_DIR/views" ]]; then
	for spec in "$UI_DIR"/views/*.md; do
		[[ -f "$spec" ]] || continue
		base="$(basename "$spec" .md)"
		[[ "$base" == "README" ]] && continue
		[[ -n "$slug" && "$base" != "$slug" ]] && continue
		case "$listed" in
			*" $base "*) ;;
			*) fail "$base: spec file views/$base.md is not in the catalogue" ;;
		esac
	done
fi

if ((errors > 0)); then
	printf 'ui-spec-check: FAILED with %d problem(s)\n' "$errors" >&2
	exit 1
fi
printf 'ui-spec-check: OK (%d view(s) checked, %d artifact(s) pending)\n' "$checked" "$pending"
