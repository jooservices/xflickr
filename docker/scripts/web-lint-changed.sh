#!/usr/bin/env bash
# Lints the web source files changed since origin/develop with the strict limits
# of web/eslint.config.js (JOOXF-505). CI job web and docker/scripts/lint.sh both
# run this script, so they check the same files the same way.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

if ! git -C "$ROOT" rev-parse --verify --quiet origin/develop >/dev/null; then
	printf 'web-lint-changed: origin/develop is missing; run git fetch origin develop first\n' >&2
	exit 1
fi

# --relative makes the paths relative to web/, where eslint runs.
diff_files="$(git -C "$ROOT/web" diff --relative --diff-filter=ACMR --name-only origin/develop... -- src)"

changed=()
while IFS= read -r file; do
	if [[ "$file" =~ \.tsx?$ ]]; then
		changed+=("$file")
	fi
done <<<"$diff_files"

if ((${#changed[@]} == 0)); then
	printf 'web-lint-changed: no changed web source files since origin/develop\n'
	exit 0
fi

cd "$ROOT/web"
"$SCRIPT_DIR/run.sh" node npx eslint "${changed[@]}"
printf 'web-lint-changed: OK (%d files)\n' "${#changed[@]}"
