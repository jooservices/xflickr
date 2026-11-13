#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
BASE="${COMMIT_BASE:-origin/develop}"
HEAD="${COMMIT_HEAD:-HEAD}"

if ! git -C "$ROOT" rev-parse --verify "$BASE^{commit}" >/dev/null 2>&1; then
	BASE="$(git -C "$ROOT" merge-base origin/develop "$HEAD")"
fi

cd "$ROOT"
"$SCRIPT_DIR/run.sh" node npm exec --yes --package=@commitlint/cli@19.8.1 -- commitlint \
	--config .github/commitlint.config.mjs --from "$BASE" --to "$HEAD"

if [[ -n "${PR_TITLE:-}" ]]; then
	printf '%s\n' "$PR_TITLE" | "$SCRIPT_DIR/run.sh" node npm exec --yes --package=@commitlint/cli@19.8.1 -- commitlint \
		--config .github/commitlint.config.mjs
fi
