#!/usr/bin/env bash
# Idempotently provision XFlickr alert delivery and alerts; also provision optional saved views and dashboard assets when present.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
ENV_FILE="${1:-$ROOT/.env.develop}"
if [[ ! -f "$ENV_FILE" ]]; then printf 'Environment file is missing.\n' >&2; exit 1; fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
URL="${OPENOBSERVE_PROVISION_URL:-http://127.0.0.1:15080}"
ORG="${OPENOBSERVE_ORG:-default}"
USERNAME="${OPENOBSERVE_USERNAME:-}"
PASSWORD="${OPENOBSERVE_PASSWORD:-}"
if [[ -z "$USERNAME" || -z "$PASSWORD" ]]; then printf 'OpenObserve credentials are required in the environment file.\n' >&2; exit 1; fi
host="${URL#*://}"; host="${host%%/*}"; host="${host%%:*}"
mkdir -p "$ROOT/.tmp"
tmp="$(mktemp -d "$ROOT/.tmp/obs-provision.XXXXXX")"; trap 'rm -rf "$tmp"' EXIT
netrc="$tmp/openobserve.netrc"
umask 077
printf 'machine %s login %s password %s\n' "$host" "$USERNAME" "$PASSWORD" > "$netrc"
auth=(--netrc-file "$netrc")

stream_type=logs
configured_stream="${OPENOBSERVE_STREAM:-xflickr}"
if [[ ! "$configured_stream" =~ ^[A-Za-z0-9_-]+$ ]]; then
	printf 'OpenObserve stream names may contain only letters, digits, underscores, or hyphens.\n' >&2
	exit 1
fi
curl --silent --show-error --fail "${auth[@]}" "${URL%/}/api/${ORG}/streams?fetchSchema=false&type=${stream_type}" > "$tmp/streams.json"
stream_names=(xflickr_logs xflickr_calls xflickr_metrics)
if [[ " ${stream_names[*]} " != *" $configured_stream "* ]]; then stream_names+=("$configured_stream"); fi
for stream_name in "${stream_names[@]}"; do
		if "$SCRIPT_DIR/run.sh" python python3 - "$tmp/streams.json" "$stream_name" <<'PY'
import json,sys
data=json.load(open(sys.argv[1])); rows=data if isinstance(data,list) else data.get("list",[])
raise SystemExit(0 if any(row.get("name")==sys.argv[2] for row in rows) else 1)
PY
		then continue; fi
		if [[ "$stream_name" == "$configured_stream" ]]; then
			printf '{"fields":[{"name":"_timestamp","type":"Int64"},{"name":"event","type":"Utf8"}],"settings":{}}' > "$tmp/stream.json"
		else
			printf '{"fields":[{"name":"_timestamp","type":"Int64"}],"settings":{}}' > "$tmp/stream.json"
		fi
		status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request POST --header 'Content-Type: application/json' --data-binary "@$tmp/stream.json" "${URL%/}/api/${ORG}/streams/${stream_name}?type=${stream_type}")"
		if [[ "$status" != 2* ]]; then printf 'Stream %s was not created (HTTP %s).\n' "$stream_name" "$status" >&2; exit 1; fi
	done

ALERT_TEMPLATE="${OPENOBSERVE_ALERT_TEMPLATE:-xflickr_telegram_v1}"
ALERT_DESTINATION="${OPENOBSERVE_ALERT_DESTINATION:-xflickr_telegram}"
SOURCE_DESTINATION="${OPENOBSERVE_ALERT_SOURCE_DESTINATION:-javmeta_telegram}"
for name in "$ALERT_TEMPLATE" "$ALERT_DESTINATION" "$SOURCE_DESTINATION"; do
	if [[ ! "$name" =~ ^[A-Za-z0-9_-]+$ ]]; then
		printf 'OpenObserve alert names may contain only letters, digits, underscores, or hyphens.\n' >&2
		exit 1
	fi
done

ensure_alert_delivery() {
	local destination="$1" template="$2" source_destination="$3"
	local destination_file="$tmp/destination.json"
	local source_file="$tmp/source-destination.json"
	local template_file="$tmp/template.json"
	local source_template_file="$tmp/source-template.json"
	local status source_status template_status dest_method dest_endpoint template_method template_endpoint
	dest_method=POST
	dest_endpoint="${URL%/}/api/${ORG}/alerts/destinations"
	template_method=POST
	template_endpoint="${URL%/}/api/${ORG}/alerts/templates"

	status="$(curl --silent --show-error --output "$destination_file" --write-out '%{http_code}' "${auth[@]}" "${URL%/}/api/${ORG}/alerts/destinations/${destination}")"
	if [[ "$status" == 404 ]]; then
		source_status="$(curl --silent --show-error --output "$source_file" --write-out '%{http_code}' "${auth[@]}" "${URL%/}/api/${ORG}/alerts/destinations/${source_destination}")"
		if [[ "$source_status" != 2* ]]; then
			printf 'OpenObserve source destination %s is missing (HTTP %s); set OPENOBSERVE_ALERT_SOURCE_DESTINATION or create %s first.\n' "$source_destination" "$source_status" "$destination" >&2
			exit 1
		fi
		cp "$source_file" "$destination_file"
	elif [[ "$status" != 2* ]]; then
		printf 'OpenObserve destination %s could not be read (HTTP %s).\n' "$destination" "$status" >&2
		exit 1
	else
		dest_method=PUT
		dest_endpoint="${URL%/}/api/${ORG}/alerts/destinations/${destination}"
	fi

	template_status="$(curl --silent --show-error --output "$template_file" --write-out '%{http_code}' "${auth[@]}" "${URL%/}/api/${ORG}/alerts/templates/${template}")"
	if [[ "$template_status" == 404 ]]; then
		source_status="$(curl --silent --show-error --output "$source_template_file" --write-out '%{http_code}' "${auth[@]}" "${URL%/}/api/${ORG}/alerts/templates/${source_destination}")"
		if [[ "$source_status" != 2* ]]; then
			printf 'OpenObserve source template %s is missing (HTTP %s); set OPENOBSERVE_ALERT_SOURCE_DESTINATION or create %s first.\n' "$source_destination" "$source_status" "$template" >&2
			exit 1
		fi
	elif [[ "$template_status" != 2* ]]; then
		printf 'OpenObserve template %s could not be read (HTTP %s).\n' "$template" "$template_status" >&2
		exit 1
	else
		template_method=PUT
		template_endpoint="${URL%/}/api/${ORG}/alerts/templates/${template}"
	fi

	"$SCRIPT_DIR/run.sh" python python3 - "$template" "$template_file" "$source_template_file" "$tmp/template-payload.json" <<'PY'
import json, sys
name, existing_path, source_path, out_path = sys.argv[1:5]
def load(path):
    try:
        return json.load(open(path))
    except FileNotFoundError:
        return None
def telegram_chat_id(template):
    if not template:
        return ''
    try:
        body = json.loads(template.get('body') or '{}')
        return str(body.get('chat_id') or '')
    except (TypeError, ValueError):
        return ''
chat_id = telegram_chat_id(load(existing_path)) or telegram_chat_id(load(source_path))
if not chat_id:
    raise SystemExit('No Telegram chat_id is available in the source template.')
text = '[XFlickr] [{alert_level}] {alert_name}\n{alert_description}\nstream={stream_name} | value={alert_agg_value} | threshold={alert_operator} {alert_threshold}\ntime={alert_trigger_time_str}\n{rows}'
body = json.dumps({'chat_id': chat_id, 'text': text}, separators=(',', ':'))
json.dump({'name': name, 'body': body, 'isDefault': False, 'type': 'http', 'title': '', 'kind': 'custom'}, open(out_path, 'w'))
PY
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request "$template_method" --header 'Content-Type: application/json' --data-binary "@$tmp/template-payload.json" "$template_endpoint")"
	if [[ "$status" != 2* ]]; then
		printf 'OpenObserve template %s was not applied (HTTP %s).\n' "$template" "$status" >&2
		exit 1
	fi

	"$SCRIPT_DIR/run.sh" python python3 - "$destination_file" "$destination" "$template" "$tmp/destination-payload.json" <<'PY'
import json, sys
source = json.load(open(sys.argv[1]))
payload = {key: source[key] for key in ('url', 'method', 'skip_tls_verify', 'emails', 'type', 'metadata') if key in source}
payload['name'] = sys.argv[2]
payload['template'] = sys.argv[3]
payload.setdefault('type', 'http')
payload.setdefault('method', 'post')
payload.setdefault('emails', [])
json.dump(payload, open(sys.argv[4], 'w'))
PY
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request "$dest_method" --header 'Content-Type: application/json' --data-binary "@$tmp/destination-payload.json" "$dest_endpoint")"
	if [[ "$status" != 2* ]]; then
		printf 'OpenObserve destination %s was not applied (HTTP %s).\n' "$destination" "$status" >&2
		exit 1
	fi
	printf 'OpenObserve delivery %s -> %s: applied\n' "$template" "$destination"
}

if [[ "$ALERT_DESTINATION" == "xflickr_telegram" ]]; then
	ensure_alert_delivery "$ALERT_DESTINATION" "$ALERT_TEMPLATE" "$SOURCE_DESTINATION"
fi
if [[ -z "$ALERT_DESTINATION" ]]; then
	curl --silent --show-error --fail "${auth[@]}" "${URL%/}/api/${ORG}/alerts/destinations" > "$tmp/destinations.json"
	ALERT_DESTINATION="$("$SCRIPT_DIR/run.sh" python python3 - "$tmp/destinations.json" <<'PY'
import json,sys
data=json.load(open(sys.argv[1])); rows=data if isinstance(data,list) else data.get("list",data.get("destinations",[]))
preferred=next((row for row in rows if row.get("name")=="xflickr_default"),None)
row=preferred or (rows[0] if rows else None)
if row: print(row.get("name", ""))
PY
	)"
fi
if [[ -z "$ALERT_DESTINATION" ]]; then
	printf 'No OpenObserve alert destination exists; configure one or set OPENOBSERVE_ALERT_DESTINATION.\n' >&2
	exit 1
fi

for file in "$ROOT"/deploy/openobserve/alerts/*.json; do
	name="$(sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$file" | head -n 1)"
	alert_body="$tmp/alert.json"
	"$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/obs_alert_config.py" render \
		"$file" "$alert_body" "$configured_stream" "$ALERT_DESTINATION"
	stream="$("$SCRIPT_DIR/run.sh" python python3 "$SCRIPT_DIR/obs_alert_config.py" stream "$alert_body")"
	if [[ -z "$stream" ]]; then printf 'Alert %s has no stream name.\n' "$name" >&2; exit 1; fi
	curl --silent --show-error --fail "${auth[@]}" "${URL%/}/api/v2/${ORG}/alerts?stream_type=logs&stream_name=${stream}" > "$tmp/alerts.json"
	alert_id=$("$SCRIPT_DIR/run.sh" python python3 - "$tmp/alerts.json" "$name" <<'PY'
import json,sys
data=json.load(open(sys.argv[1])); rows=data if isinstance(data,list) else data.get("list",[])
for row in rows:
    if row.get("name")==sys.argv[2]: print(row.get("alert_id",row.get("id",""))); break
PY
	)
	if [[ -n "$alert_id" ]]; then
		method=PUT; endpoint="${URL%/}/api/v2/${ORG}/alerts/${alert_id}"
	else
		method=POST; endpoint="${URL%/}/api/v2/${ORG}/alerts"
	fi
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request "$method" --header 'Content-Type: application/json' --data-binary "@$alert_body" "$endpoint")"
	if [[ "$status" != 2* ]]; then printf 'Alert %s was not applied (HTTP %s).\n' "$name" "$status" >&2; exit 1; fi
done

if [[ ! -d "$ROOT/deploy/openobserve/queries" || ! -f "$ROOT/deploy/openobserve/dashboard.json" ]]; then
	printf 'XFlickr OpenObserve alerts provisioned.\n'
	exit 0
fi

curl --silent --show-error --fail "${auth[@]}" "${URL%/}/api/${ORG}/savedviews" > "$tmp/views.json"
for file in "$ROOT"/deploy/openobserve/queries/*.json; do
	view_name="$(sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$file" | head -n 1)"
	view_id=$("$SCRIPT_DIR/run.sh" python python3 - "$tmp/views.json" "$view_name" <<'PY'
import json,sys
data=json.load(open(sys.argv[1]))
views=data if isinstance(data,list) else data.get("views",data.get("data",[]))
for row in views:
    if row.get("view_name")==sys.argv[2]: print(row.get("view_id", "")); break
PY
	)
	body="$tmp/view.json"
	"$SCRIPT_DIR/run.sh" python python3 - "$file" "$body" <<'PY'
import base64,json,sys
source=json.load(open(sys.argv[1]))
payload={"version":1,"query":{"sql":source["sql"],"stream":source["stream"],"stream_type":"logs"}}
json.dump({"view_name":source["name"],"view_type":"logs","data":base64.b64encode(json.dumps(payload,separators=(",",":")).encode()).decode()},open(sys.argv[2],"w"))
PY
	if [[ -n "$view_id" ]]; then method=PUT; endpoint="${URL%/}/api/${ORG}/savedviews/${view_id}"; else method=POST; endpoint="${URL%/}/api/${ORG}/savedviews"; fi
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request "$method" --header 'Content-Type: application/json' --data-binary "@$body" "$endpoint")"
	if [[ "$status" != 2* ]]; then printf 'Saved query %s was not applied (HTTP %s).\n' "$view_name" "$status" >&2; exit 1; fi
done

curl --silent --show-error --fail "${auth[@]}" "${URL%/}/api/${ORG}/dashboards?title=XFlickr%20Crawl%20Operations" > "$tmp/dashboards.json"
dashboard_id="$("$SCRIPT_DIR/run.sh" python python3 - "$tmp/dashboards.json" <<'PY'
import json,sys
data=json.load(open(sys.argv[1])); rows=data if isinstance(data,list) else data.get("dashboards",[])
for row in rows:
    if row.get("title")=="XFlickr Crawl Operations": print(row.get("dashboard_id",row.get("id",""))); break
PY
)"
dashboard_hash=""
if [[ -n "$dashboard_id" ]]; then
	curl --silent --show-error --fail "${auth[@]}" "${URL%/}/api/${ORG}/dashboards/${dashboard_id}?folder=default" > "$tmp/dashboard-current.json"
	dashboard_hash="$("$SCRIPT_DIR/run.sh" python python3 - "$tmp/dashboard-current.json" <<'PY'
import json,sys
row=json.load(open(sys.argv[1])); print(row.get("hash", ""))
PY
)"
fi
dashboard="$tmp/dashboard.json"
"$SCRIPT_DIR/run.sh" python python3 - "$ROOT/deploy/openobserve/queries" "$ROOT/deploy/openobserve/dashboard.json" "$dashboard" <<'PY'
import glob,json,os,sys
items=[json.load(open(path)) for path in sorted(glob.glob(os.path.join(sys.argv[1],"*.json")))]
dashboard=json.load(open(sys.argv[2])); panels=[]
for index,item in enumerate(items):
    panels.append({"id":f"xflickr-{index+1}","type":"table","title":item["name"],"description":item["sql"],"config":{"show_legends":False},"queries":[{"fields":{"stream":item["stream"],"stream_type":"logs","x":[],"y":[],"z":[],"filter":{"type":"list","values":[],"logicalOperator":"AND","filterType":"list"}},"customQuery":True,"query":{"sql":item["sql"]}}]})
dashboard["panels"]=panels
json.dump(dashboard,open(sys.argv[3],"w"))
PY
if [[ -n "$dashboard_id" ]]; then
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request PUT --header 'Content-Type: application/json' --data-binary "@$dashboard" "${URL%/}/api/${ORG}/dashboards/${dashboard_id}?folder=default&hash=${dashboard_hash}")"
else
	status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${auth[@]}" --request POST --header 'Content-Type: application/json' --data-binary "@$dashboard" "${URL%/}/api/${ORG}/dashboards")"
fi
if [[ "$status" != 2* ]]; then printf 'Dashboard was not applied (HTTP %s).\n' "$status" >&2; exit 1; fi
printf 'XFlickr OpenObserve alerts, saved queries, and dashboard provisioned.\n'
