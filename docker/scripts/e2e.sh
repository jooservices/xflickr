#!/usr/bin/env bash
# Real full-stack browser E2E (docs/e2e.md). Builds the backend, fakeflickr and the web bundle, resets the worktree
# database, starts the stack on free local ports and runs Playwright (headless Chrome on the host) against it.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
STACK_DIR="$ROOT/.tmp/e2e"

if [[ ! -f "$ROOT/web/package.json" ]]; then
	printf 'No web end-to-end suite is configured yet.\n'
	exit 0
fi

node_bin="$(command -v node || true)"
if [[ -z "$node_bin" ]]; then
	printf 'Web end-to-end tests need Node.js on the host to select free ports.\n' >&2
	exit 1
fi
if [[ ! -f "$ROOT/.env" ]]; then
	printf 'The real-stack E2E needs the generated worktree .env (shared PostgreSQL and Redis).\n' >&2
	exit 1
fi

# Only a worktree database may be reset. The live database xflickr is refused here and again below.
"$SCRIPT_DIR/dev.sh" verify-db >/dev/null
env_value() {
	sed -n "s/^$1=//p" "$ROOT/.env" | head -n 1 | sed -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'\$/\1/"
}
db_name="$(env_value DB_DATABASE)"
db_user="$(env_value DB_USERNAME)"
case "$db_name" in
	xflickr_wt*) ;;
	*)
		printf 'Refusing to reset database %s: only xflickr_wt* worktree databases are allowed.\n' "$db_name" >&2
		exit 1
		;;
esac

free_port() {
	"$node_bin" -e 'const s = require("node:net").createServer(); s.listen(0, "127.0.0.1", () => { process.stdout.write(String(s.address().port)); s.close(); });'
}

api_port="$(free_port)"
fake_port="$(free_port)"
run_id="e2e$(date +%s)"
admin_password="$(openssl rand -hex 12)"

mkdir -p "$STACK_DIR"
pids=()
cleanup() {
	rm -rf "$ROOT/web/.e2e-state"
	for pid in "${pids[@]}"; do
		kill "$pid" 2>/dev/null || true
	done
	wait 2>/dev/null || true
}
trap cleanup EXIT

# E2E_SKIP_BUILD=1 reuses the binaries and bundle of the previous run while iterating on the specs.
if [[ "${E2E_SKIP_BUILD:-}" != 1 ]]; then
	printf 'Building backend, fakeflickr and web bundle...\n'
	"$SCRIPT_DIR/run.sh" go sh -c "cd '$ROOT/backend' && CGO_ENABLED=0 go build -o '$STACK_DIR/xflickr' ./cmd/xflickr && CGO_ENABLED=0 go build -o '$STACK_DIR/fakeflickr' ./cmd/fakeflickr"
	(cd "$ROOT/web" && "$SCRIPT_DIR/run.sh" node npm run build >/dev/null 2>&1)
fi

printf 'Resetting worktree database %s...\n' "$db_name"
postgres_container="${E2E_POSTGRES_CONTAINER:-workspaces-shared-postgres-1}"
docker exec -i -e PGPASSWORD="$(env_value DB_PASSWORD)" -e PGOPTIONS="-c client_min_messages=warning" "$postgres_container" \
	psql -v ON_ERROR_STOP=1 -q -U "$db_user" -d "$db_name" \
	-c 'DROP SCHEMA public CASCADE' -c 'CREATE SCHEMA public' >/dev/null

# Host-side overrides of the generated .env, which names the container-network hosts. The Redis prefix is unique per run.
export DB_HOST=127.0.0.1 DB_PORT=15435 REDIS_HOST=127.0.0.1 REDIS_PORT=16382
REDIS_PREFIX="$(env_value REDIS_PREFIX)${run_id}:"
export REDIS_PREFIX
export NODE_ID=e2e OPENSEARCH_INDEX_PREFIX="xflickr_${run_id}_"
master_key="$(openssl rand -base64 32 | tr -d '\n')"
export XFLICKR_MASTER_KEYS="e2e-v1:${master_key}" XFLICKR_MASTER_KEY_ACTIVE=e2e-v1
export HTTP_PORT="$api_port" XFLICKR_WEB_DIR="$ROOT/web/dist" XFLICKR_ROLES=api,scheduler,crawl
export BLOB_LOCAL_DIR="$STACK_DIR/blobs"
export FLICKR_OAUTH_CALLBACK_URL="http://127.0.0.1:${api_port}/flickr-accounts/callback"
export FLICKR_UPSTREAM_URL="http://127.0.0.1:${fake_port}"
# A shared Argon2 cost that keeps registration fast; production keeps its own defaults.
export ARGON2ID_MEMORY=8192 ARGON2ID_ITERATIONS=1

cd "$ROOT"
"$STACK_DIR/xflickr" migrate up >"$STACK_DIR/migrate.log" 2>&1 || {
	cat "$STACK_DIR/migrate.log" >&2
	exit 1
}
FAKEFLICKR_ADDR="127.0.0.1:${fake_port}" "$STACK_DIR/fakeflickr" >"$STACK_DIR/fakeflickr.log" 2>&1 &
pids+=("$!")
"$STACK_DIR/xflickr" >"$STACK_DIR/xflickr.log" 2>&1 &
pids+=("$!")

for _ in $(seq 1 60); do
	curl --silent --fail "http://127.0.0.1:${api_port}/healthz" >/dev/null 2>&1 && break
	sleep 1
done
if ! curl --silent --fail "http://127.0.0.1:${api_port}/healthz" >/dev/null; then
	printf 'The E2E stack did not become healthy; log tail:\n' >&2
	tail -n 40 "$STACK_DIR/xflickr.log" >&2
	exit 1
fi

export E2E_BASE_URL="http://127.0.0.1:${api_port}"
export E2E_FAKEFLICKR_URL="$FLICKR_UPSTREAM_URL"
export E2E_ADMIN_EMAIL=admin@e2e.test E2E_ADMIN_PASSWORD="$admin_password"

# E2E_HOLD=1 keeps the stack up for manual debugging (curl, a browser) until the script is interrupted.
if [[ "${E2E_HOLD:-}" == 1 ]]; then
	printf 'Stack is up: %s (admin %s / %s, fakeflickr %s). Press Ctrl-C to stop.\n' "$E2E_BASE_URL" "$E2E_ADMIN_EMAIL" "$E2E_ADMIN_PASSWORD" "$E2E_FAKEFLICKR_URL"
	while true; do sleep 1; done
fi

rm -rf "$ROOT/web/.e2e-state"
"$SCRIPT_DIR/playwright.sh" "$@"
