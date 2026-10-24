#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ! -s "$1" ]]; then
	printf 'Usage: %s <coverage-profile>\nA non-empty Go coverage profile is required.\n' "$0" >&2
	exit 2
fi

printf 'Coverage report generated; threshold enforcement will be added in JOOXF-73.\n'
