# OpenObserve alert provisioning

`deploy/o2/provision.sh` manually provisions only the existing
`xflickr_quota_exhausted` alert from
`deploy/openobserve/alerts/xflickr-quota-exhausted.json` against the
Dev Workspaces OpenObserve instance in [ENVIRONMENT.md](ENVIRONMENT.md).

## Configuration

The script reads its process environment and never reads or sources `.env`.

| Variable | Required | Default |
| --- | --- | --- |
| `OPENOBSERVE_PROVISION_URL` | No | `http://127.0.0.1:15080` |
| `OPENOBSERVE_ORG` | No | `default` |
| `OPENOBSERVE_STREAM` | No | `xflickr` |
| `OPENOBSERVE_USERNAME` | Yes | — |
| `OPENOBSERVE_PASSWORD` | Yes | — |
| `OPENOBSERVE_ALERT_DESTINATION` | Yes | — |

Supply credentials through the operator's approved secret injection method.
Set non-secret overrides as needed, then run:

    export OPENOBSERVE_PROVISION_URL=http://127.0.0.1:15080
    export OPENOBSERVE_ORG=default
    export OPENOBSERVE_STREAM=xflickr
    export OPENOBSERVE_ALERT_DESTINATION=xflickr_telegram
    deploy/o2/provision.sh

The username and password must be non-empty. Org, stream and destination names
may contain only letters, digits, underscores and hyphens. The URL must be
absolute and must use HTTPS unless its host is a loopback address (`localhost`,
`127.0.0.1` or `::1`). It must not contain userinfo, a query or a fragment.
Requests use Basic Auth, a JSON content type when there is a body, and a
10-second timeout. Any non-2xx response, network error or invalid JSON fails
the run. Requests are not retried.

## Create and update

The provisioner lists alerts in the configured org and matches the exact name
`xflickr_quota_exhausted` across all streams. It POSTs the rendered payload
when no match exists. It PUTs to the ID when exactly one match has an
`alert_id` or `id`, allowing a configured stream change to update the existing
alert. Multiple matches or a missing ID fail. It never deletes alerts and does
not print credentials, headers or response bodies.

The payload comes from the one checked-in alert JSON; only stream and
destination are substituted. Both resources must already exist in OpenObserve.
The OpenObserve account needs permission to list, create and update alerts in
the selected org.

This alert detects a key whose hourly quota is exhausted; it does not detect
early exhaustion. Its query reads `crawl.health.key` events over the
five-minute period in the source JSON. It does not query PostgreSQL or
application tables.

## Test

Run the offline HTTP-stub acceptance tests:

    python3 docker/scripts/test_o2_provision.py

The tests invoke the shell entrypoint as a subprocess and use a loopback-only
`ThreadingHTTPServer`, which is shut down after each case. No PostgreSQL,
Redis, live OpenObserve instance or container is needed.

## Limits and remaining scope

This slice provisions one alert. Quota, Flickr call, crawl/frontier,
download/sync and AI dashboards; idempotent dashboard provisioning; the
early-exhaustion and node-IP-blocked alerts; and OpenObserve schema or query
semantic validation remain for later slices. No UI, database schema, REST API
or server configuration is changed.
