#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

mkdir -p "$ROOT/.tmp"

(
	cd "$ROOT/web"
	"$SCRIPT_DIR/run.sh" node npm exec -- redocly bundle ../contracts/openapi/openapi.yaml --output ../.tmp/openapi.bundle.yaml
)

(
	cd "$ROOT/backend"
	mkdir -p internal/platform/api/gen
	"$SCRIPT_DIR/run.sh" go go tool oapi-codegen -config internal/platform/api/oapi-codegen.yaml ../.tmp/openapi.bundle.yaml
)

(
	cd "$ROOT/web"
	"$SCRIPT_DIR/run.sh" node npm exec -- orval --config orval.config.ts
	"$SCRIPT_DIR/run.sh" node npm exec -- prettier --write src/shared/api/gen src/shared/api/zod
)

(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go generate ./...
)
