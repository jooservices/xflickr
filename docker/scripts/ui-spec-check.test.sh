#!/usr/bin/env bash
# Shell unit tests for ui-spec-check.sh using fixture specs (good and mutated bad copies).
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CHECK="$SCRIPT_DIR/ui-spec-check.sh"
GOOD="$SCRIPT_DIR/testdata/ui-spec-check/good"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/ui-spec-check-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

failures=0
total=0

fresh() {
	rm -rf "$WORK/ui"
	cp -R "$GOOD" "$WORK/ui"
}

# expect <name> <exit status> <stderr/stdout pattern> [args...]
expect() {
	local name="$1" want="$2" pattern="$3" out status=0
	shift 3
	total=$((total + 1))
	out="$(UI_DIR="$WORK/ui" "$CHECK" "$@" 2>&1)" || status=$?
	if [[ "$status" -ne "$want" ]] || ! printf '%s' "$out" | grep -Eq "$pattern"; then
		printf 'FAIL: %s (exit %s, want %s)\n%s\n' "$name" "$status" "$want" "$out" >&2
		failures=$((failures + 1))
	else
		printf 'ok: %s\n' "$name"
	fi
}

# edit_file <file> <sed expression>: portable in-place edit.
edit_file() {
	sed -e "$2" "$1" >"$1.tmp"
	mv "$1.tmp" "$1"
}

fresh
expect 'good fixture passes' 0 'OK \(2 view'

fresh
expect 'good slug passes' 0 'OK \(1 view' demo

fresh
expect 'strict rejects pending rows' 1 'later: spec is pending' --strict

fresh
expect 'slug check rejects pending row' 1 'later: spec is pending' later

fresh
expect 'unknown slug fails' 1 'nope: not in the catalogue' nope

fresh
rm "$WORK/ui/views/demo.md"
expect 'missing spec file fails' 1 'demo: spec file is missing'

fresh
rm "$WORK/ui/mockups/views/demo.html"
expect 'missing mockup file fails' 1 'demo: mockup file is missing'

fresh
edit_file "$WORK/ui/README.md" 's#\[spec\](views/demo.md)#-#'
expect 'row without spec fails' 1 'demo: catalogue row lacks its spec'

fresh
edit_file "$WORK/ui/README.md" 's#\[mockup\](mockups/views/demo.html)#-#'
expect 'row without mockup fails' 1 'demo: catalogue row lacks its mockup'

fresh
edit_file "$WORK/ui/views/demo.md" '/^## Accessibility/d'
expect 'spec without heading fails' 1 "lacks the heading '## Accessibility'"

fresh
edit_file "$WORK/ui/views/demo.md" '/^### Error/d'
expect 'spec without state fails' 1 "lacks the state '### Error'"

fresh
edit_file "$WORK/ui/views/demo.md" 's/data-testid/test-id/'
expect 'spec without test hook fails' 1 'no data-testid'

fresh
edit_file "$WORK/ui/mockups/views/demo.html" '/data-state="empty"/d'
expect 'mockup without state fails' 1 'lacks the state section data-state="empty"'

fresh
cp "$WORK/ui/views/demo.md" "$WORK/ui/views/stray.md"
expect 'spec not in catalogue fails' 1 'stray: spec file views/stray.md is not in the catalogue'

fresh
cp "$WORK/ui/views/demo.md" "$WORK/ui/views/later.md"
expect 'pending row with existing spec fails' 1 'later: spec is marked pending but views/later.md exists'

fresh
rm "$WORK/ui/README.md"
expect 'missing catalogue fails' 1 'catalogue not found'

printf '\n%d test(s), %d failure(s)\n' "$total" "$failures"
[[ "$failures" -eq 0 ]]
