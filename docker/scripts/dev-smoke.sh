#!/usr/bin/env bash
set -euo pipefail

network="workspaces-shared"

require_healthy_service() {
	local service="$1"
	local container_ids container_id state health

	if ! container_ids="$(docker ps --quiet \
		--filter "network=$network" \
		--filter "label=com.docker.compose.service=$service")"; then
		printf 'FAIL Docker is unavailable while checking shared %s.\n' "$service" >&2
		return 1
	fi
	if [[ -z "$container_ids" ]]; then
		printf 'FAIL shared %s is not running on network %s.\n' "$service" "$network" >&2
		return 1
	fi
	if [[ "$container_ids" == *$'\n'* ]]; then
		printf 'FAIL multiple shared %s containers found on network %s.\n' "$service" "$network" >&2
		return 1
	fi
	container_id="$container_ids"
	if ! read -r state health < <(docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_id"); then
		printf 'FAIL could not inspect shared %s.\n' "$service" >&2
		return 1
	fi
	if [[ "$state" != running || "$health" != healthy ]]; then
		printf 'FAIL shared %s is %s (%s), expected running and healthy.\n' "$service" "$state" "$health" >&2
		return 1
	fi
	printf 'PASS shared %s is healthy.\n' "$service"
}

require_healthy_service postgres
require_healthy_service redis
