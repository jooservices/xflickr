#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
COMPOSE_FILE="$ROOT/deploy/compose/develop.yml"
ENV_FILE="$ROOT/.env.develop"

find_workspaces_root() {
	local candidate="$ROOT"
	while [[ "$candidate" != / ]]; do
		if [[ -x "$candidate/scripts/docker/shared" ]]; then
			printf '%s\n' "$candidate"
			return 0
		fi
		candidate="$(dirname -- "$candidate")"
	done
	printf 'Cannot locate Workspaces scripts/docker/shared.\n' >&2
	return 1
}

workspaces_root="${WORKSPACES_ROOT:-$(find_workspaces_root)}"
shared="$workspaces_root/scripts/docker/shared"

ensure_env() {
	if [[ -L "$ENV_FILE" ]]; then
		printf 'Refusing a symlinked develop env file.\n' >&2
		return 1
	fi
	if [[ ! -f "$ENV_FILE" ]]; then
		"$shared" env-project xflickr --mode live --output "$ENV_FILE"
		chmod 600 "$ENV_FILE"
	fi
	chmod 600 "$ENV_FILE"
	"$shared" check-project xflickr --env-file "$ENV_FILE" --mode live

	if ! grep -q '^XFLICKR_MASTER_KEYS=.' "$ENV_FILE"; then
		local master_key
		master_key="$(openssl rand -base64 32 | tr -d '\n')"
		printf '\n# XFlickr application settings\nXFLICKR_MASTER_KEYS=develop-v1:%s\n' "$master_key" >> "$ENV_FILE"
		unset master_key
	fi
	ensure_setting XFLICKR_MASTER_KEY_ACTIVE develop-v1
	ensure_setting NODE_ID xflickr-develop
	ensure_setting OPENSEARCH_INDEX_PREFIX xflickr_
	ensure_setting SEARCH_URL http://elasticsearch:9200
	ensure_setting HTTP_PORT 8080
	ensure_setting BLOB_LOCAL_DIR /app/data
	ensure_setting XFLICKR_ROLES api,scheduler,indexer,crawl,media,node-agent
	chmod 600 "$ENV_FILE"
}

ensure_setting() {
	local key="$1" value="$2"
	if ! grep -q "^${key}=" "$ENV_FILE"; then
		printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
	fi
}

configure_openobserve() {
	local workspaces_env="$workspaces_root/.env"
	local -a credentials=()
	if [[ ! -f "$workspaces_env" ]]; then
		printf 'Warning: shared Workspaces private configuration is missing; leaving existing OpenObserve settings unchanged.\n' >&2
		return 0
	fi
	mapfile -t credentials < <(
		(
			# shellcheck disable=SC1090
			. "$workspaces_env"
			printf '%s\n%s\n' "${WORKSPACES_ZO_ROOT_USER_EMAIL:-}" "${WORKSPACES_ZO_ROOT_USER_PASSWORD:-}"
		)
	)
	if [[ -z "${credentials[0]:-}" || -z "${credentials[1]:-}" ]]; then
		printf 'Warning: shared OpenObserve credentials are not configured in private Workspaces settings; leaving existing OpenObserve settings unchanged.\n' >&2
		return 0
	fi
	set_private_setting OPENOBSERVE_URL http://openobserve:5080
	set_private_setting OPENOBSERVE_ORG default
	set_private_setting OPENOBSERVE_STREAM xflickr
	set_private_setting OPENOBSERVE_USERNAME "${credentials[0]}"
	set_private_setting OPENOBSERVE_PASSWORD "${credentials[1]}"
	set_private_setting OPENOBSERVE_ALERT_DESTINATION javmeta_telegram
	unset credentials
}

set_private_setting() {
	local key="$1" value="$2" tmp replaced=0
	if grep -Fqx -- "$key=$value" "$ENV_FILE"; then
		return 0
	fi
	tmp="$(mktemp "$ROOT/.env.develop.XXXXXX")"
	while IFS= read -r line || [[ -n "$line" ]]; do
		if [[ "$line" == "$key="* ]]; then
			if (( replaced == 0 )); then
				printf '%s=%s\n' "$key" "$value" >> "$tmp"
				replaced=1
			fi
		else
			printf '%s\n' "$line" >> "$tmp"
		fi
	done < "$ENV_FILE"
	if (( replaced == 0 )); then
		printf '%s=%s\n' "$key" "$value" >> "$tmp"
	fi
	chmod 600 "$tmp"
	mv "$tmp" "$ENV_FILE"
}

compose() {
	docker compose --project-name xflickr-develop --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

case "${1:-}" in
	up)
		ensure_env
		configure_openobserve
		chmod 600 "$ENV_FILE"
		compose up --build -d
		;;
	down)
		ensure_env
		compose down
		;;
	logs)
		ensure_env
		compose logs --follow --tail=100 xflickr
		;;
	status)
		ensure_env
		compose ps --all
		;;
	*)
		printf 'Usage: %s {up|down|logs|status}\n' "$0" >&2
		exit 2
		;;
esac
