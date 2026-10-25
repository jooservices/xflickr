#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
# The checker runs in the shared runtime, which sees the repository but not the host /tmp.
mkdir -p "$ROOT/.tmp"
TEMP_DIR="$(mktemp -d "$ROOT/.tmp/api-usage.XXXXXX")"
trap 'rm -rf "$TEMP_DIR"' EXIT

mkdir -p "$TEMP_DIR/contracts/openapi" "$TEMP_DIR/backend/internal/platform/api/gen" "$TEMP_DIR/web/src"
cat >"$TEMP_DIR/contracts/openapi/openapi.yaml" <<'YAML'
paths:
  /known:
    get: {}
YAML
cat >"$TEMP_DIR/backend/internal/platform/api/gen/server.gen.go" <<'GO'
r.Get(options.BaseURL+"/known", handler)
GO

check() {
	API_USAGE_ROOT="$TEMP_DIR" "$SCRIPT_DIR/api-usage-check.sh" >/dev/null 2>&1
}

printf '%s\n' "export const ok = 1" >"$TEMP_DIR/web/src/clean.ts"
check || { printf 'api-usage-check rejected a clean web tree\n' >&2; exit 1; }

printf '%s\n' "request('/missing')" >"$TEMP_DIR/web/src/violation.ts"
if check; then
	printf 'api-usage-check accepted a seeded web path missing from OpenAPI\n' >&2
	exit 1
fi

printf '%s\n' "fetch('/known')" >"$TEMP_DIR/web/src/violation.ts"
if check; then
	printf 'api-usage-check accepted a hand-written call to a contract path\n' >&2
	exit 1
fi
