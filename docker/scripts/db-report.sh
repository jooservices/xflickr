#!/usr/bin/env bash
# Read-only database report for the xflickr database (JOOXF-373).
# Usage: db-report.sh [--env-file FILE] [--deep]
#   --env-file FILE  file with DB_HOST, DB_PORT, DB_USER, DB_PASSWORD and DB_NAME (default: .env in the repository root)
#   --deep           also run the exact (slower) counts
# The env file is parsed line by line and never sourced, so odd quoting cannot run code.
# The session is read-only: PGOPTIONS sets default_transaction_read_only=on.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
ENV_FILE="$ROOT/.env"
DEEP=()

while (($#)); do
	case "$1" in
		--env-file) ENV_FILE="${2:?--env-file needs a path}"; shift 2 ;;
		--deep) DEEP=(-v deep=1); shift ;;
		-h|--help) sed -n '2,8p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) printf 'db-report: unknown argument: %s\n' "$1" >&2; exit 2 ;;
	esac
done

[[ -f "$ENV_FILE" ]] || { printf 'db-report: env file not found: %s\n' "$ENV_FILE" >&2; exit 2; }

declare -A cfg=()
while IFS= read -r line || [[ -n "$line" ]]; do
	[[ "$line" =~ ^[[:space:]]*(DB_HOST|DB_PORT|DB_USER|DB_PASSWORD|DB_NAME|DB_DATABASE)=(.*)$ ]] || continue
	key="${BASH_REMATCH[1]}"
	value="${BASH_REMATCH[2]}"
	value="${value%\"}"; value="${value#\"}"; value="${value%\'}"; value="${value#\'}"
	cfg["$key"]="$value"
done <"$ENV_FILE"

host="${cfg[DB_HOST]:-127.0.0.1}"
port="${cfg[DB_PORT]:-5432}"
user="${cfg[DB_USER]:-postgres}"
name="${cfg[DB_NAME]:-${cfg[DB_DATABASE]:-xflickr}}"

PGPASSWORD="${cfg[DB_PASSWORD]:-}" PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=60000' \
	psql -X -q -h "$host" -p "$port" -U "$user" -d "$name" "${DEEP[@]}" -v ON_ERROR_STOP=0 -f "$SCRIPT_DIR/db-report.sql"
