#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

status="$(git -C "$ROOT" status --porcelain --untracked-files=all)"
if [[ -n "$status" ]]; then
	printf 'Generated code is stale. Run make gen and commit the result.\n' >&2
	printf '%s\n' "$status" >&2
	git -C "$ROOT" diff --stat >&2
	exit 1
fi
