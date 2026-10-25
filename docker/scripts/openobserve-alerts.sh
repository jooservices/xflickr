#!/usr/bin/env bash
# Creates or updates the XFlickr crawl-health alerts in the shared OpenObserve.
# Reads OPENOBSERVE_URL, OPENOBSERVE_USERNAME, OPENOBSERVE_PASSWORD, OPENOBSERVE_ORG
# and OPENOBSERVE_STREAM from the environment file given as $1 (default .env.develop).
# The password is passed to curl through a netrc file on stdin, never on the command line.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
ENV_FILE="${1:-$ROOT/.env.develop}"
ALERT_DIR="$ROOT/deploy/openobserve/alerts"

if [[ ! -f "$ENV_FILE" ]]; then
	printf 'Environment file %s is missing.\n' "$ENV_FILE" >&2
	exit 1
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
URL="${OPENOBSERVE_URL:-http://127.0.0.1:15080}"
ORG="${OPENOBSERVE_ORG:-default}"
STREAM="${OPENOBSERVE_STREAM:-xflickr}"
if [[ -z "${OPENOBSERVE_USERNAME:-}" || -z "${OPENOBSERVE_PASSWORD:-}" ]]; then
	printf 'OPENOBSERVE_USERNAME and OPENOBSERVE_PASSWORD are required in %s.\n' "$ENV_FILE" >&2
	exit 1
fi
host="${URL#*://}"
host="${host%%/*}"
host="${host%%:*}"

for file in "$ALERT_DIR"/*.json; do
	name="$(basename "$file" .json)"
	body="${file}"
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
		--netrc-file <(printf 'machine %s login %s password %s\n' "$host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
		--request POST --header 'Content-Type: application/json' --data-binary "@$body" \
		"${URL%/}/api/${ORG}/${STREAM}/alerts/${name}")"
	if [[ "$status" == 409 || "$status" == 400 ]]; then
		status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
			--netrc-file <(printf 'machine %s login %s password %s\n' "$host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
			--request PUT --header 'Content-Type: application/json' --data-binary "@$body" \
			"${URL%/}/api/${ORG}/${STREAM}/alerts/${name}")"
	fi
	if [[ "$status" != 2* ]]; then
		printf 'Alert %s was not applied (HTTP %s).\n' "$name" "$status" >&2
		exit 1
	fi
	printf 'Alert %s: ok\n' "$name"
done
