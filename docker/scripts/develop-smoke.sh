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
if ! curl --fail --silent --show-error -H 'Accept: text/html' "$BASE_URL/keys" | grep -qi '<html'; then
	printf 'Keys page failed.\n' >&2
	exit 1
fi
printf 'Web index, registration, and keys pages: ok\n'

ops_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "$BASE_URL/admin/ops/status")"
if [[ "$ops_status" != 401 && "$ops_status" != 403 ]]; then
	printf 'Admin ops status endpoint should require authentication (got HTTP %s).\n' "$ops_status" >&2
	exit 1
fi
if [[ -n "${XFLICKR_ADMIN_COOKIE:-}" ]]; then
	if ! curl --fail --silent --show-error --header "Cookie: $XFLICKR_ADMIN_COOKIE" "$BASE_URL/admin/ops/status" | grep -q '"calls_last_10min"'; then
		printf 'Admin ops status did not return the crawl health report.\n' >&2
		exit 1
	fi
fi
printf 'Admin ops status endpoint: ok\n'

container_logs="$("${compose[@]}" logs --no-color --tail 500 xflickr 2>/dev/null)"
if ! grep -q 'crawl health' <<<"$container_logs"; then
	printf 'No "crawl health" log line found; the scheduler role logs one at start and every minute.\n' >&2
	exit 1
fi
unset container_logs
printf 'Crawl health log line: ok\n'

# Query OpenObserve directly so the smoke check proves ingestion, not only local
# logging. Response bodies stay in a temporary file and event values are not printed.
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
observe_url="${OPENOBSERVE_URL:-http://127.0.0.1:15080}"
if [[ "$observe_url" == http://openobserve:5080* ]]; then
	observe_url="${observe_url/http:\/\/openobserve:5080/http:\/\/127.0.0.1:15080}"
fi
if [[ -z "${OPENOBSERVE_USERNAME:-}" || -z "${OPENOBSERVE_PASSWORD:-}" ]]; then
	printf 'OpenObserve credentials are unavailable; cannot verify stream ingestion.\n' >&2
	exit 1
fi
if [[ -z "${OPENOBSERVE_ALERT_DESTINATION:-}" ]]; then
	printf 'OpenObserve notification destination is not configured.\n' >&2
	exit 1
fi
observe_host="${observe_url#*://}"
observe_host="${observe_host%%/*}"
observe_host="${observe_host%%:*}"
now_us="$(($(date +%s) * 1000000))"
start_us="$((now_us - 900000000))"
sql='SELECT * FROM "'"${OPENOBSERVE_STREAM:-xflickr}"'" WHERE event IN ('"'crawl.health'"', '"'crawl.health.key'"') ORDER BY _timestamp DESC LIMIT 100'
query="$(jq -nc --arg sql "$sql" --argjson start "$start_us" --argjson end "$now_us" \
	'{query:{sql:$sql,start_time:$start,end_time:$end,from:0,size:100}}')"
response="$(mktemp)"
trap 'rm -f "$response"' EXIT
if ! curl --silent --show-error --fail \
	--netrc-file <(printf 'machine %s login %s password %s\n' "$observe_host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
	--header 'Content-Type: application/json' --data "$query" \
	"${observe_url%/}/api/${OPENOBSERVE_ORG:-default}/_search" > "$response"; then
	printf 'OpenObserve stream query failed.\n' >&2
	exit 1
fi
if ! jq -e '
	.hits as $hits
	| ([$hits[]? | select(.event == "crawl.health")]
		| any((has("pending_items") and has("ready_jobs") and has("calls_last_10min")
			and has("failed_total") and has("tokens_revoked") and has("token_status")
			and has("people") and has("photos") and has("photo_sizes")
			and has("favorites") and has("albums") and has("galleries") and has("groups")
			and has("work_items_pending") and has("river_jobs_available"))))
	and ([$hits[]? | select(.event == "crawl.health.key")]
		| any((has("key_id") and has("used") and has("remaining") and has("limit"))))
' "$response" >/dev/null; then
	printf 'Recent OpenObserve results are missing crawl.health or crawl.health.key events/fields.\n' >&2
	exit 1
fi
rm -f "$response"
trap - EXIT
unset OPENOBSERVE_PASSWORD
printf 'OpenObserve crawl health and per-key events: ok\n'
# Sweep every route of the app in a real browser against the deployed container: any uncaught exception, console
# error, failed request, HTML where JSON was expected or error page fails the deploy. The sweep only reads.
if [[ -z "${XFLICKR_SMOKE_EMAIL:-}" || -z "${XFLICKR_SMOKE_PASSWORD:-}" ]]; then
	printf 'Route sweep requires XFLICKR_SMOKE_EMAIL and XFLICKR_SMOKE_PASSWORD for an existing develop admin.\n' >&2
	exit 1
fi

rm -rf "$ROOT/web/.e2e-state"
sweep_env=(
	E2E_BASE_URL="$BASE_URL"
	E2E_ADMIN_EMAIL="$XFLICKR_SMOKE_EMAIL"
	E2E_ADMIN_PASSWORD="$XFLICKR_SMOKE_PASSWORD"
)
printf 'Route sweep: signed in as the smoke admin.\n'
if ! env "${sweep_env[@]}" "$SCRIPT_DIR/playwright.sh" e2e/99-route-sweep.spec.ts; then
	printf 'Route sweep failed: a screen of the deployed app is broken.\n' >&2
	exit 1
fi
rm -rf "$ROOT/web/.e2e-state"
printf 'Route sweep: ok\n'
