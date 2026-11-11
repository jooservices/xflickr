#!/usr/bin/env bash
# Shell tests for security-audit-web.sh using a fake npm executable.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
CHECK="$SCRIPT_DIR/security-audit-web.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/security-audit-web-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

failures=0
total=0

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}

run_case() {
	local name="$1" fake_status="$2" want_status="$3" caller_dir="$4"
	local status=0
	local previous_failures="$failures"
	total=$((total + 1))
	: >"$WORK/cwd"
	: >"$WORK/args"
	(
		cd -- "$caller_dir"
		PATH="$WORK/bin:$PATH" FAKE_NPM_STATUS="$fake_status" FAKE_NPM_WORK="$WORK" bash "$CHECK"
	) >"$WORK/output" 2>&1 || status=$?
	if [[ "$status" -ne "$want_status" ]]; then
		fail "$name: exit $status, wanted $want_status"
	fi
	if [[ "$(cat "$WORK/cwd")" != "$ROOT/web" ]]; then
		fail "$name: npm ran from $(cat "$WORK/cwd"), wanted $ROOT/web"
	fi
	if ! diff -u "$WORK/expected-args" "$WORK/args" >"$WORK/diff"; then
		fail "$name: npm arguments differed from the expected audit command"
	fi
	if [[ "$failures" -eq "$previous_failures" ]]; then
		printf 'ok: %s\n' "$name"
	fi
}

mkdir -p "$WORK/bin" "$WORK/elsewhere"
cat >"$WORK/bin/npm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$PWD" >"$FAKE_NPM_WORK/cwd"
printf '%s\n' "$@" >"$FAKE_NPM_WORK/args"
exit "$FAKE_NPM_STATUS"
EOF
chmod +x "$WORK/bin/npm"
printf 'audit\n--audit-level=high\n' >"$WORK/expected-args"

run_case 'web audit passes' 0 0 "$ROOT"
run_case 'web audit propagates failure' 1 1 "$ROOT"
run_case 'web audit targets lockfile directory from elsewhere' 0 0 "$WORK/elsewhere"
run_case 'web audit uses high threshold' 0 0 "$ROOT"

printf '\n%d test(s), %d failure(s)\n' "$total" "$failures"
[[ "$failures" -eq 0 ]]
