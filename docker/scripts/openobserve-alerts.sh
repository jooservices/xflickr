#!/usr/bin/env bash
# Compatibility wrapper: canonical provisioning lives in obs-provision.sh.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
ENV_FILE="${1:-$ROOT/.env.develop}"

printf 'openobserve-alerts.sh is deprecated; delegating to obs-provision.sh.\n' >&2
exec "$SCRIPT_DIR/obs-provision.sh" "$ENV_FILE"
