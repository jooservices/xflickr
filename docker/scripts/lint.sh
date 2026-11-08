#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.15.0}"

"$SCRIPT_DIR/run.sh" shellcheck shellcheck "$SCRIPT_DIR"/*.sh
"$SCRIPT_DIR/api-usage-check.test.sh"
"$SCRIPT_DIR/ui-e2e-coverage.test.sh"
"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/test_obs_alert_config.py"
"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/test_worktree_env.py"
"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/test_check_go_version.py"
"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/test_sqlc_stale.py"
"$SCRIPT_DIR/api-usage-check.sh"
"$SCRIPT_DIR/ui-spec-check.sh"
(
	cd "$ROOT/backend"
	"$SCRIPT_DIR/run.sh" go go vet ./...
	"$SCRIPT_DIR/run.sh" go go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$GOLANGCI_LINT_VERSION" \
		run --config "$ROOT/.golangci.yml" ./...
)

if [[ -f "$ROOT/web/package.json" ]]; then
	(
		cd "$ROOT/web"
		"$SCRIPT_DIR/run.sh" node npm run lint
	)
fi
