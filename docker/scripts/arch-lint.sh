#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

GO_ARCH_LINT_VERSION="${GO_ARCH_LINT_VERSION:-v1.19.0}"

(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go run "github.com/fe3dback/go-arch-lint@$GO_ARCH_LINT_VERSION" \
		check --project-path "$ROOT/backend" --arch-file "$ROOT/.go-arch-lint.yml"
)
