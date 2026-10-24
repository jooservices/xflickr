#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
	printf 'Usage: %s <tool> <command> [args...]\n' "$0" >&2
	exit 2
fi

tool="$1"
shift

case "$tool" in
	go|node|python|media|gitleaks|php|php@8.1|php@8.2|php@8.3|php@8.4|php@8.5)
		if [[ -n "${WORKSPACES_RUNTIME:-}" ]]; then
			exec "$WORKSPACES_RUNTIME" "$tool" "$@"
		fi
		;;
esac

exec "$@"
