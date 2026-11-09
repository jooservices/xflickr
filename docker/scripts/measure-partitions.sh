#!/usr/bin/env bash
# Run EXPLAIN (ANALYZE, BUFFERS) probes against the synthetic catalog.
# Requires docker/scripts/synthetic-catalog.sh to have finished.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
while IFS= read -r line || [[ -n "$line" ]]; do
  case "$line" in ''|\#*) continue ;; esac
  key="${line%%=*}"; value="${line#*=}"; export "$key=$value"
done < .env
case "${DB_NAME:-}" in xflickr_wt*) ;; *) echo "refuse ${DB_NAME:-}" >&2; exit 1 ;; esac
export PGPASSWORD="${DB_PASSWORD}"
OUT="${1:-/home/joos/Workspaces/.tmp/jooxf372/explain.txt}"
mkdir -p "$(dirname "$OUT")"
{
  echo "# measured at $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  for i in 1 2 3 4 5; do
    echo "## run $i"
    psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f docker/scripts/measure-partitions.sql
  done
} >"$OUT" 2>&1
echo "wrote $OUT"
