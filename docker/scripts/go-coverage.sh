#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
PROFILE_DIR="$ROOT/.tmp/go-coverage"
profile="$ROOT/.tmp/xflickr-coverage.out"

mapfile -t profiles < <(find "$PROFILE_DIR" -maxdepth 1 -type f -name '*.out' -print | sort)
expected="$(python3 "$SCRIPT_DIR/integration_shards.py" go-groups | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')"
if [[ ${#profiles[@]} -ne "$expected" ]]; then
	printf 'Expected %s Go coverage shards, found %s.\n' "$expected" "${#profiles[@]}" >&2
	exit 1
fi

python3 "$SCRIPT_DIR/merge_coverage.py" "$profile" "${profiles[@]}"
"$SCRIPT_DIR/coverage-gate.sh" "$profile"
python3 "$SCRIPT_DIR/coverage_gate.py" "$profile" --exclude "$SCRIPT_DIR/coverage-exclude.txt" \
	--check-files --files-only --base "${COVERAGE_BASE:-origin/develop}" \
	--baseline "$SCRIPT_DIR/coverage-file-baseline.txt"
