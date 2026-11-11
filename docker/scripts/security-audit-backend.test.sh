#!/usr/bin/env bash
# Shell tests for security-audit-backend.sh using a fake go executable.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
CHECK="$SCRIPT_DIR/security-audit-backend.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/security-audit-backend-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

failures=0
total=0

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}

# run_case <name> <fake go exit status> <wanted exit status> <caller dir> <expected args file> [extra env...]
run_case() {
	local name="$1" fake_status="$2" want_status="$3" caller_dir="$4" expected="$5"
	shift 5
	local status=0
	local previous_failures="$failures"
	total=$((total + 1))
	: >"$WORK/cwd"
	: >"$WORK/args"
	(
		cd -- "$caller_dir"
		env "$@" PATH="$WORK/bin:$PATH" FAKE_GO_STATUS="$fake_status" FAKE_GO_WORK="$WORK" bash "$CHECK"
	) >"$WORK/output" 2>&1 || status=$?
	if [[ "$status" -ne "$want_status" ]]; then
		fail "$name: exit $status, wanted $want_status"
	fi
	if [[ "$(cat "$WORK/cwd")" != "$ROOT/backend" ]]; then
		fail "$name: go ran from $(cat "$WORK/cwd"), wanted $ROOT/backend"
	fi
	if ! diff -u "$expected" "$WORK/args" >"$WORK/diff"; then
		fail "$name: go arguments differed from the expected govulncheck command"
	fi
	if [[ "$failures" -eq "$previous_failures" ]]; then
		printf 'ok: %s\n' "$name"
	fi
}

mkdir -p "$WORK/bin" "$WORK/elsewhere"
cat >"$WORK/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$PWD" >"$FAKE_GO_WORK/cwd"
printf '%s\n' "$@" >"$FAKE_GO_WORK/args"
exit "$FAKE_GO_STATUS"
EOF
chmod +x "$WORK/bin/go"
printf 'run\ngolang.org/x/vuln/cmd/govulncheck@v1.9.0\n./...\n' >"$WORK/expected-default"
printf 'run\ngolang.org/x/vuln/cmd/govulncheck@v9.9.9\n./...\n' >"$WORK/expected-override"

run_case 'backend scan passes' 0 0 "$ROOT" "$WORK/expected-default"
run_case 'backend scan propagates a vulnerability finding' 3 3 "$ROOT" "$WORK/expected-default"
run_case 'backend scan targets the module directory from elsewhere' 0 0 "$WORK/elsewhere" "$WORK/expected-default"
run_case 'backend scan pins the tool version by default' 0 0 "$ROOT" "$WORK/expected-default"
run_case 'backend scan version can be overridden explicitly' 0 0 "$ROOT" "$WORK/expected-override" GOVULNCHECK_VERSION=v9.9.9

printf '\n%d test(s), %d failure(s)\n' "$total" "$failures"
[[ "$failures" -eq 0 ]]
