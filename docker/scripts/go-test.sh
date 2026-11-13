#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
BACKEND="$ROOT/backend"
PROFILE_DIR="$ROOT/.tmp/go-coverage"

mkdir -p "$PROFILE_DIR"

run_group() {
	local group="$1"
	local profile="$PROFILE_DIR/$group.out"
	mapfile -t packages < <(python3 "$SCRIPT_DIR/integration_shards.py" go-packages "$group")
	if [[ ${#packages[@]} -eq 0 ]]; then
		printf 'Go test group %s has no packages.\n' "$group" >&2
		exit 1
	fi
	printf '\n==> Go tests: %s (%d packages)\n' "$group" "${#packages[@]}"
	(
		cd "$BACKEND"
		"$SCRIPT_DIR/run.sh" go go test -count=1 -coverprofile="$profile" "${packages[@]}"
	)
}

if [[ -n "${TEST_GROUP:-}" ]]; then
	run_group "$TEST_GROUP"
else
	groups="$(python3 "$SCRIPT_DIR/integration_shards.py" go-groups | python3 -c 'import json,sys; print("\n".join(json.load(sys.stdin)))')"
	while IFS= read -r group; do
		[[ -z "$group" ]] || run_group "$group"
	done <<<"$groups"
fi
