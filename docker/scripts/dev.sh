#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

find_workspaces_root() {
	local candidate="$ROOT"
	while [[ "$candidate" != / ]]; do
		if [[ -x "$candidate/scripts/docker/shared" ]]; then
			printf '%s\n' "$candidate"
			return 0
		fi
		candidate="$(dirname "$candidate")"
	done
	printf 'Cannot locate Workspaces scripts/docker/shared.\n' >&2
	return 1
}

verify_worktree_database() {
	local workspaces_root shared_script database_name worktree_id
	workspaces_root="${WORKSPACES_ROOT:-$(find_workspaces_root)}"
	shared_script="$workspaces_root/scripts/docker/shared"
	if [[ ! -x "$shared_script" ]]; then
		printf 'Workspaces shared-services helper is unavailable.\n' >&2
		return 1
	fi
	if [[ ! -f "$ROOT/.env" ]]; then
		printf 'A generated worktree .env is required.\n' >&2
		return 1
	fi

	database_name="$("$SCRIPT_DIR/run.sh" python python3 -c '
from pathlib import Path
import sys

for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines():
    key, separator, value = line.partition("=")
    if separator and key.strip() in ("DB_NAME", "DB_DATABASE"):
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in (chr(34), chr(39)):
            value = value[1:-1]
        print(value)
        break
' "$ROOT/.env")"
	case "$database_name" in
		xflickr_wt*) worktree_id="${database_name#xflickr_wt}" ;;
		*)
			printf 'The generated .env must select this worktree database.\n' >&2
			return 1
			;;
	esac
	if [[ -z "$worktree_id" ]]; then
		printf 'The generated .env has an invalid worktree database name.\n' >&2
		return 1
	fi

	"$shared_script" check-project xflickr --env-file "$ROOT/.env" --mode "wt:$worktree_id"
}

command="${1:-}"
case "$command" in
	up)
		"$SCRIPT_DIR/dev-smoke.sh"
		verify_worktree_database
		printf 'Shared development services and worktree database are ready.\n'
		;;
	verify-db)
		verify_worktree_database
		;;
	down)
		printf 'No XFlickr-owned development services are started by dev-up.\n'
		;;
	*)
		printf 'Usage: %s {up|verify-db|down}\n' "$0" >&2
		exit 2
		;;
esac
