#!/usr/bin/env bash
# Fixture tests for compare-db.sh with a fake psql (no database needed).
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
mkdir -p "$ROOT/.tmp"
WORK="$(mktemp -d "$ROOT/.tmp/compare-db.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

cat >"$WORK/bin/psql" <<'SH'
#!/usr/bin/env bash
# fake psql: arguments are logged, answers depend on the database name (-d) and the query (-c)
db=""; sql=""; args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
	case "${args[i]}" in -d) db="${args[i + 1]}";; -c) sql="${args[i + 1]}";; esac
done
printf '%s|%s|%s|%s\n' "$db" "${PGPASSWORD:-}" "${PGOPTIONS:-}" "$*" >>"$FAKE_PSQL_LOG"
case "$sql" in
	*pg_stat_user_tables*) printf 'public.a\npublic.b\npublic.c\n' ;;
	*"from public.a"*) echo 10 ;;
	*"from public.b"*) [[ "$db" == dstdb ]] && echo "${FAKE_B_DST:-5}" || echo 5 ;;
	*"from public.c"*) [[ "$db" == dstdb && -n "${FAKE_C_MISSING:-}" ]] && exit 1 || echo 7 ;;
esac
SH
chmod +x "$WORK/bin/psql"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
ok() { printf 'ok: %s\n' "$*"; }
cat >"$WORK/src.env" <<'ENV'
DB_HOST=src.example
DB_PORT=5432
DB_USER=u1
DB_PASSWORD="p@ss word $(touch WORKPWNED)"
DB_NAME=srcdb
ENV
sed -i "s#WORKPWNED#$WORK/pwned#" "$WORK/src.env"
cat >"$WORK/dst.env" <<'ENV'
DB_HOST=dst.example
DB_PORT=5433
DB_USER=u2
DB_PASSWORD=secret2
DB_NAME=dstdb
DB_SSLMODE=require
ENV
run() { PATH="$WORK/bin:$PATH" FAKE_PSQL_LOG="$WORK/log" "$SCRIPT_DIR/compare-db.sh" --src-env "$WORK/src.env" --dst-env "$WORK/dst.env" "$@"; }

out="$(run)"; echo "$out" | grep -q "3 tables compared, 0 different" || fail "equal databases"
ok "equal databases pass"
if FAKE_B_DST=4 run >"$WORK/out" 2>&1; then fail "different counts must fail"; fi
grep -q "public.b.*DIFF" "$WORK/out" || fail "diff line"
ok "a differing table fails and is named"
if FAKE_C_MISSING=1 run >"$WORK/out" 2>&1; then fail "missing table must fail"; fi
grep -q "public.c.*ERR.*DIFF" "$WORK/out" || fail "missing table reported"
ok "a table that cannot be counted fails"
grep -q 'default_transaction_read_only=on' "$WORK/log" || fail "read-only option"
ok "sessions are read-only"
[[ ! -e "$WORK/pwned" ]] || fail "env file content was executed"
ok "env files are parsed, never executed"
if grep -E '(p@ss word|secret2)' "$WORK/log" | awk -F'|' '{print $4}' | grep -qE '(p@ss word|secret2)'; then fail "password found in the command arguments"; fi
ok "passwords never appear in the command arguments"
if run --bogus >/dev/null 2>&1; then fail "unknown argument"; fi
ok "unknown argument fails"
printf '%s\n' "7 test(s), 0 failure(s)"
