#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

# Verifies that the worktree .env selects its own database and that PostgreSQL,
# Redis and search answer. XFlickr starts no database, cache or search container.
verify_worktree_database() {
	"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/worktree_env.py" --check --worktree "$ROOT"
}

command="${1:-}"
case "$command" in
	up | verify-db)
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
