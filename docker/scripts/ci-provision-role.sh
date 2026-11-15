#!/usr/bin/env bash
# Give the CI integration database an owner that is not an administrator, like the account of a worktree tree.
set -euo pipefail

: "${DB_HOST:?}" "${DB_PORT:?}" "${DB_NAME:?}" "${DB_USER:?}" "${DB_PASSWORD:?}" "${CI_PG_ADMIN_PASSWORD:?}"

PGPASSWORD="$CI_PG_ADMIN_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U postgres -d "$DB_NAME" \
	-v ON_ERROR_STOP=1 -v role="$DB_USER" -v password="$DB_PASSWORD" -v database="$DB_NAME" <<'SQL'
CREATE ROLE :"role" LOGIN PASSWORD :'password' NOSUPERUSER NOCREATEDB NOCREATEROLE;
ALTER DATABASE :"database" OWNER TO :"role";
SQL
