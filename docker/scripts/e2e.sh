#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

if [[ ! -f "$ROOT/web/package.json" ]]; then
	printf 'No web end-to-end suite is configured yet.\n'
	exit 0
fi

cd "$ROOT/web"

find_available_port() {
	local node_bin
	node_bin="$(command -v node || true)"
	if [[ -z "$node_bin" ]]; then
		printf 'Web end-to-end tests need Node.js to select an available port.\n' >&2
		exit 1
	fi
	"$node_bin" -e 'const net = require("node:net"); const server = net.createServer(); server.listen(0, "127.0.0.1", () => { const address = server.address(); if (address && typeof address !== "string") process.stdout.write(String(address.port)); server.close(); });'
}

export E2E_PORT="${E2E_PORT:-$(find_available_port)}"

# Playwright needs a browser. The shared node runtime does not have one, and the
# host cannot open a server that listens only inside that container. When Chrome
# is on the host, run the suite there. GitHub Actions leaves WORKSPACES_RUNTIME
# unset, so run.sh uses the native toolchain and Playwright's own browser.
if [[ -n "${WORKSPACES_RUNTIME:-}" ]]; then
	if ! command -v google-chrome >/dev/null 2>&1; then
		printf 'Web end-to-end tests need Google Chrome on the host.\n' >&2
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
	PLAYWRIGHT_CHROME_CHANNEL=chrome "$npm_bin" run e2e
	exit 0
fi

"$SCRIPT_DIR/run.sh" node npm run e2e
