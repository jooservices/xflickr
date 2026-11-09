#!/usr/bin/env bash
# Compare the exact row count of every table of two databases (JOOXF-382 migration check).
# Usage: compare-db.sh --src-env FILE --dst-env FILE [--jobs N]
#   FILE holds DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME and optional DB_SSLMODE (parsed line by line, never sourced).
# Prints one line per table (table | source | target | OK or DIFF) and exits 1 when anything differs or is missing.
# Read-only: the sessions open with default_transaction_read_only=on.
set -euo pipefail

SRC=""; DST=""; JOBS=4
while (($#)); do
	case "$1" in
		--src-env) SRC="${2:?}"; shift 2 ;;
		--dst-env) DST="${2:?}"; shift 2 ;;
		--jobs) JOBS="${2:?}"; shift 2 ;;
		-h|--help) sed -n '2,7p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) printf 'compare-db: unknown argument: %s\n' "$1" >&2; exit 2 ;;
	esac
done
[[ -f "$SRC" && -f "$DST" ]] || { printf 'compare-db: --src-env and --dst-env must be existing files\n' >&2; exit 2; }

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

# Run one query against the database described by an env file. The password goes through the environment only.
q() {
	local file="$1" sql="$2" host="127.0.0.1" port="5432" user="postgres" name="" ssl="prefer" pass=""
	local line key value
	while IFS= read -r line || [[ -n "$line" ]]; do
		[[ "$line" =~ ^[[:space:]]*(DB_HOST|DB_PORT|DB_USER|DB_PASSWORD|DB_NAME|DB_SSLMODE)=(.*)$ ]] || continue
		key="${BASH_REMATCH[1]}"; value="${BASH_REMATCH[2]}"
		value="${value%\"}"; value="${value#\"}"; value="${value%\'}"; value="${value#\'}"
		case "$key" in DB_HOST) host="$value";; DB_PORT) port="$value";; DB_USER) user="$value";; DB_PASSWORD) pass="$value";; DB_NAME) name="$value";; DB_SSLMODE) ssl="$value";; esac
	done <"$file"
	PGPASSWORD="$pass" PGSSLMODE="$ssl" PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=0' \
		psql -X -q -At -h "$host" -p "$port" -U "$user" -d "$name" -c "$sql"
}
export -f q

tables="$(q "$SRC" "select quote_ident(schemaname) || '.' || quote_ident(relname) from pg_stat_user_tables order by 1")"
[[ -n "$tables" ]] || { printf 'compare-db: no tables found in the source\n' >&2; exit 2; }

# shellcheck disable=SC2317 # invoked indirectly through xargs and bash -c
count_one() { # file table -> count or ERR
	local c; c="$(q "$1" "select count(*) from $2" 2>/dev/null)" && printf '%s' "$c" || printf 'ERR'
}
export -f count_one

# shellcheck disable=SC2016 # the single-quoted script is intentionally expanded by the inner bash
printf '%s\n' "$tables" | xargs -P "$JOBS" -I{} bash -c 'printf "%s|%s|%s\n" "{}" "$(count_one "$0" "{}")" "$(count_one "$1" "{}")"' "$SRC" "$DST" >"$WORK/counts.txt"

status=0; total=0; bad=0
while IFS='|' read -r table a b; do
	total=$((total + 1))
	if [[ "$a" == "$b" && "$a" != "ERR" ]]; then verdict="OK"; else verdict="DIFF"; bad=$((bad + 1)); status=1; fi
	printf '%-48s %14s %14s  %s\n' "$table" "$a" "$b" "$verdict"
done < <(sort "$WORK/counts.txt")
printf '%s\n' "compare-db: $total tables compared, $bad different"
exit "$status"
