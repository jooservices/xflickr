#!/usr/bin/env bash
# Fixture tests for db-report.sh: a fake psql records how it was called; no database is needed.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
mkdir -p "$ROOT/.tmp"
WORK="$(mktemp -d "$ROOT/.tmp/db-report.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/bin"
cat >"$WORK/bin/psql" <<'SH'
#!/usr/bin/env bash
{
	printf 'args:%s\n' "$*"
	printf 'PGPASSWORD=%s\n' "${PGPASSWORD:-}"
	printf 'PGOPTIONS=%s\n' "${PGOPTIONS:-}"
} >"$FAKE_PSQL_LOG"
SH
chmod +x "$WORK/bin/psql"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
ok() { printf 'ok: %s\n' "$*"; }

run() { PATH="$WORK/bin:$PATH" FAKE_PSQL_LOG="$WORK/psql.log" "$SCRIPT_DIR/db-report.sh" "$@"; }

cat >"$WORK/env" <<'ENV'
# comment line
DB_HOST=db.example
DB_PORT=15435
DB_USER="report_user"
DB_PASSWORD='p$ss"word with space & $(touch '"$WORK"'/pwned)'
DB_NAME=xflickr_x
OTHER_SECRET=must-not-leak
ENV

run --env-file "$WORK/env" >/dev/null
grep -q -- '-h db.example -p 15435 -U report_user -d xflickr_x' "$WORK/psql.log" || fail "connection arguments"
ok "reads host, port, user and database from the env file"

grep -q 'PGOPTIONS=-c default_transaction_read_only=on' "$WORK/psql.log" || fail "read-only session"
ok "opens a read-only session"

[[ ! -e "$WORK/pwned" ]] || fail "env file content was executed"
# shellcheck disable=SC2016 # the pattern is a literal, nothing must expand
grep -q 'PGPASSWORD=p\$ss"word with space & \$(touch' "$WORK/psql.log" || fail "password kept literally"
ok "never executes the env file and keeps the password literally"

if grep -q 'must-not-leak' "$WORK/psql.log"; then fail "unrelated variables leaked"; fi
ok "ignores unrelated variables"

run --env-file "$WORK/env" --deep >/dev/null
grep -q -- '-v deep=1' "$WORK/psql.log" || fail "--deep flag"
ok "--deep passes the deep variable"

if run --env-file "$WORK/missing" >/dev/null 2>&1; then fail "missing env file must fail"; fi
ok "missing env file fails"

if run --bogus >/dev/null 2>&1; then fail "unknown argument must fail"; fi
ok "unknown argument fails"

run --help | grep -q 'Read-only database report' || fail "help text"
ok "help prints usage"

# The SQL file must never contain a write statement.
if grep -Eiq '^[[:space:]]*(insert|update|delete|drop|alter|create|truncate|vacuum|reindex|grant)[[:space:]]' "$SCRIPT_DIR/db-report.sql"; then
	fail "db-report.sql contains a write statement"
fi
ok "the SQL contains no write statement"

printf "%s\n" "9 test(s), 0 failure(s)"
