#!/usr/bin/env bash
# Shell unit tests for ui-e2e-coverage.sh using fixture catalogue and coverage docs.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CHECK="$SCRIPT_DIR/ui-e2e-coverage.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/ui-e2e-coverage-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

failures=0
total=0

fresh() {
	rm -rf "$WORK/root"
	mkdir -p "$WORK/root/web/e2e"
	cat >"$WORK/root/catalogue.md" <<'EOF'
# Catalogue
| id | role |
| --- | --- |
| `demo` | user |
| `demo-two` | admin |
EOF
	cat >"$WORK/root/coverage.md" <<'EOF'
# Coverage
| view slug | scenario file | scenario id | test ids |
| --- | --- | --- | --- |
| `demo` | `web/e2e/demo.spec.ts` | `e2e/user/demo.spec.ts` | `demo-page` |
| `demo-two` | `web/e2e/demo-two.spec.ts` | `e2e/admin/demo-two.spec.ts` | `demo-two-page` |
EOF
	cat >"$WORK/root/baseline.txt" <<'EOF'
# Baseline
EOF
	touch "$WORK/root/web/e2e/demo.spec.ts" "$WORK/root/web/e2e/demo-two.spec.ts"
}

run_checker() {
	CATALOGUE="$WORK/root/catalogue.md" \
	COVERAGE_DOC="$WORK/root/coverage.md" \
	E2E_COVERAGE_ROOT="$WORK/root" \
	E2E_COVERAGE_BASELINE="$WORK/root/baseline.txt" \
	UI_E2E_COVERAGE="${1:-fail}" \
	"$CHECK" 2>&1
}

# expect <name> <exit status> <output pattern> [mode]
expect() {
	local name="$1" want="$2" pattern="$3" out status=0
	shift 3
	total=$((total + 1))
	out="$(run_checker "${1:-fail}")" || status=$?
	if [[ "$status" -ne "$want" ]] || ! printf '%s' "$out" | grep -Eq "$pattern"; then
		printf 'FAIL: %s (exit %s, want %s)\n%s\n' "$name" "$status" "$want" "$out" >&2
		failures=$((failures + 1))
	else
		printf 'ok: %s\n' "$name"
	fi
}

fresh
expect 'all mapped passes' 0 '2 covered, 0 missing \(0 known gaps, 0 new\), 0 deferred'

fresh
printf '%s\n' "| \`demo-missing\` | public |" >>"$WORK/root/catalogue.md"
expect 'catalogue view without a row fails' 1 'demo-missing: no row in the coverage document'

fresh
cat >"$WORK/root/coverage.md" <<'EOF'
# Coverage
| view slug | scenario file | scenario id | test ids |
| --- | --- | --- | --- |
| `demo` | `web/e2e/demo.spec.ts` | `e2e/user/demo.spec.ts` | `demo-page` |
| `demo-two` | `MISSING` | `e2e/admin/demo-two.spec.ts` | `demo-two-page` |
EOF
expect 'MISSING row without deferred fails' 1 'demo-two: scenario is MISSING'

fresh
rm "$WORK/root/web/e2e/demo.spec.ts"
expect 'mapped file missing fails' 1 'demo: mapped scenario file is missing'

fresh
cat >"$WORK/root/coverage.md" <<'EOF'
# Coverage
| view slug | scenario file | scenario id | test ids |
| --- | --- | --- | --- |
| `demo` | `web/e2e/demo.spec.ts` | `e2e/user/demo.spec.ts` | `demo-page` |
| `demo-two` | `MISSING` | `deferred: view not built yet` | `demo-two-page` |
EOF
expect 'deferred row passes' 0 '1 deferred'

fresh
printf '%s\n' "| \`demo-missing\` | public |" >>"$WORK/root/catalogue.md"
expect 'warn mode exits 0 with a warning' 0 'WARNING.*demo-missing' warn

fresh
cat >"$WORK/root/coverage.md" <<'EOF'
# Coverage
| view slug | scenario file | scenario id | test ids |
| --- | --- | --- | --- |
| `demo` | `web/e2e/demo.spec.ts` | `e2e/user/demo.spec.ts` | `demo-page` |
| `demo-two` | `MISSING` | `e2e/admin/demo-two.spec.ts` | `demo-two-page` |
EOF
printf '%s\n' "demo-two" >>"$WORK/root/baseline.txt"
expect 'known gap passes' 0 'known gap: demo-two'

fresh
cat >"$WORK/root/coverage.md" <<'EOF'
# Coverage
| view slug | scenario file | scenario id | test ids |
| --- | --- | --- | --- |
| `demo` | `web/e2e/demo.spec.ts` | `e2e/user/demo.spec.ts` | `demo-page` |
| `demo-two` | `MISSING` | `e2e/admin/demo-two.spec.ts` | `demo-two-page` |
| `demo-three` | `MISSING` | `e2e/admin/demo-three.spec.ts` | `demo-three-page` |
EOF
printf '%s\n' "| \`demo-three\` | admin |" >>"$WORK/root/catalogue.md"
printf '%s\n' "demo-two" >>"$WORK/root/baseline.txt"
expect 'new missing view fails' 1 'uncovered: demo-three'

fresh
printf '%s\n' "demo" >>"$WORK/root/baseline.txt"
expect 'baseline entry now covered fails' 1 'Baseline view now covered: demo \(delete its baseline line\)'

fresh
printf '%s\n' "ghost-view" >>"$WORK/root/baseline.txt"
expect 'stale baseline entry fails' 1 'stale baseline entry: ghost-view'

fresh
printf '%s\n' "demo" >>"$WORK/root/baseline.txt"
expect 'warn mode with a covered baseline entry still exits 0' 0 'Baseline view now covered: demo' warn

fresh
rm "$WORK/root/catalogue.md"
expect 'missing catalogue fails' 1 'catalogue not found'

printf '\n%d test(s), %d failure(s)\n' "$total" "$failures"
[[ "$failures" -eq 0 ]]
