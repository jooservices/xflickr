#!/usr/bin/env bash
# Runs `npm run e2e -- <args>` in web/. Playwright needs a browser, which the shared node runtime does not have,
# so with WORKSPACES_RUNTIME it runs on the host with Google Chrome (Playwright never runs in Docker). GitHub
# Actions leaves WORKSPACES_RUNTIME unset, so run.sh uses the native toolchain and Playwright's own browser.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

cd "$ROOT/web"
if [[ ! -d node_modules ]]; then
	"$SCRIPT_DIR/run.sh" node npm ci >/dev/null
fi

if [[ -n "${WORKSPACES_RUNTIME:-}" ]]; then
	if ! command -v google-chrome >/dev/null 2>&1; then
		printf 'Web end-to-end tests need Google Chrome on the host (Playwright never runs in Docker).\n' >&2
		exit 1
	fi
	npm_bin="$(command -v npm || true)"
	if [[ -z "$npm_bin" && -x "${HOME}/.local/bin/npm" ]]; then
		npm_bin="${HOME}/.local/bin/npm"
	fi
	if [[ -z "$npm_bin" ]]; then
		printf 'Web end-to-end tests need npm on the host so Playwright can launch Chrome.\n' >&2
		exit 1
	fi
	npm_dir="$(dirname "$npm_bin")"
	export PATH="${npm_dir}:${PATH}"
	PLAYWRIGHT_CHROME_CHANNEL=chrome "$npm_bin" run e2e -- "$@"
else
	"$SCRIPT_DIR/run.sh" node npm run e2e -- "$@"
fi
