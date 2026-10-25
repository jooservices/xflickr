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
# shellcheck disable=SC1090
. "$ENV_FILE"
URL="${OPENOBSERVE_ALERTS_URL:-${OPENOBSERVE_URL:-http://127.0.0.1:15080}}"
# Runtime containers use the shared Docker DNS name. This script runs on the
# Workspaces host, so route that service URL through its published host port.
if [[ "$URL" == http://openobserve:5080* ]]; then
	URL="${URL/http:\/\/openobserve:5080/http:\/\/127.0.0.1:15080}"
fi
ORG="${OPENOBSERVE_ORG:-default}"
STREAM="${OPENOBSERVE_STREAM:-xflickr}"
if [[ -z "${OPENOBSERVE_USERNAME:-}" || -z "${OPENOBSERVE_PASSWORD:-}" ]]; then
	printf 'OPENOBSERVE_USERNAME and OPENOBSERVE_PASSWORD are required in %s.\n' "$ENV_FILE" >&2
	exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
	printf 'jq is required to verify OpenObserve alert definitions.\n' >&2
	exit 1
fi
host="${URL#*://}"
host="${host%%/*}"
host="${host%%:*}"
alerts_url="${URL%/}/api/v2/${ORG}/alerts"

for file in "$ALERT_DIR"/*.json; do
	name="$(basename "$file" .json)"
	if ! jq -e '((.destinations // []) | length > 0) or ((.workflows // []) | length > 0)' "$file" >/dev/null; then
		printf 'Alert %s cannot be applied: this OpenObserve API requires a configured destination or workflow.\n' "$name" >&2
		exit 1
	fi
	alert_id="$(curl --silent --show-error --fail \
		--netrc-file <(printf 'machine %s login %s password %s\n' "$host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
		"$alerts_url" | jq -r --arg name "$name" '.list[]? | select(.name == $name) | .alert_id // empty' | head -n 1)"
	method=POST
	endpoint="$alerts_url"
	if [[ -n "$alert_id" ]]; then
		method=PUT
		endpoint="$alerts_url/$alert_id"
	fi
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
		--netrc-file <(printf 'machine %s login %s password %s\n' "$host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
		--request "$method" --header 'Content-Type: application/json' --data-binary "@$file" \
		"$endpoint")"
	if [[ "$status" != 2* ]]; then
		printf 'Alert %s was not applied (HTTP %s).\n' "$name" "$status" >&2
		exit 1
	fi
	definition="$(curl --silent --show-error --fail \
		--netrc-file <(printf 'machine %s login %s password %s\n' "$host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
		"$alerts_url" | jq -c --arg name "$name" '.list[]? | select(.name == $name) | .alert_id' | head -n 1)"
	if [[ -z "$definition" || "$definition" == null ]]; then
		printf 'Alert %s was applied but is missing from the alert list.\n' "$name" >&2
		exit 1
	fi
	alert_id="$(jq -r '.' <<<"$definition")"
	definition="$(curl --silent --show-error --fail \
		--netrc-file <(printf 'machine %s login %s password %s\n' "$host" "$OPENOBSERVE_USERNAME" "$OPENOBSERVE_PASSWORD") \
		"$alerts_url/$alert_id")"
	if ! jq -e --arg name "$name" --arg stream "$STREAM" --slurpfile expected "$file" \
		'.name == $name and .enabled == true and .stream_name == $stream and .stream_type == "logs" and .query_condition.sql == $expected[0].query_condition.sql' \
		<<<"$definition" >/dev/null; then
		printf 'Alert %s was applied but its active configuration does not match the tracked definition.\n' "$name" >&2
		exit 1
	fi
	printf 'Alert %s: applied and verified enabled\n' "$name"
done
