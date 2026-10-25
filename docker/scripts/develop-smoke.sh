#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
ENV_FILE="$ROOT/.env.develop"
COMPOSE_FILE="$ROOT/deploy/compose/develop.yml"
BASE_URL="${XFLICKR_DEVELOP_URL:-http://127.0.0.1:18490}"

if [[ ! -f "$ENV_FILE" ]]; then
	printf 'Develop env file is missing; run make develop-up first.\n' >&2
	exit 1
fi
if ! grep -q '^DB_DATABASE=xflickr$' "$ENV_FILE" || ! grep -q '^REDIS_DB=7$' "$ENV_FILE"; then
	printf 'Develop env file does not select the live XFlickr database and Redis index.\n' >&2
	exit 1
fi

compose=(docker compose --project-name xflickr-develop --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
container_id="$("${compose[@]}" ps -q xflickr)"
if [[ -z "$container_id" ]] || [[ "$(docker inspect --format '{{.State.Health.Status}}' "$container_id" 2>/dev/null || true)" != healthy ]]; then
	printf 'XFlickr develop container is not healthy.\n' >&2
	exit 1
fi
printf 'Container health: ok\n'

if ! curl --fail --silent --show-error "$BASE_URL/healthz" >/dev/null; then
	printf 'Health endpoint failed.\n' >&2
	exit 1
fi
printf 'Health endpoint: ok\n'

migration_status="$("${compose[@]}" exec -T xflickr /app/xflickr migrate status 2>/dev/null)" || {
	printf 'Could not read migration status from database xflickr.\n' >&2
	exit 1
}
if grep -qi 'pending' <<<"$migration_status" || ! grep -qi 'applied' <<<"$migration_status"; then
	printf 'Database xflickr has pending migrations.\n' >&2
	exit 1
fi
printf 'Database migrations: applied\n'

if ! curl --fail --silent --show-error "$BASE_URL/" | grep -qi '<html'; then
	printf 'Web index page failed.\n' >&2
	exit 1
fi
if ! curl --fail --silent --show-error "$BASE_URL/register" | grep -qi '<html'; then
	printf 'Registration page failed.\n' >&2
	exit 1
fi
printf 'Web index and registration pages: ok\n'
