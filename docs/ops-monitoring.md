# Ops monitoring

The crawl runs unattended, so the application reports its own health instead of
requiring SQL. Three outputs share one status query (`internal/platform/ops`).

## Crawl health log line

The `scheduler` role logs `crawl health` once at start and then every minute
(`docker logs xflickr-develop`). Fields:

| Field | Meaning |
|---|---|
| `people`, `photos`, `photo_sizes`, `favorites`, `albums`, `galleries`, `groups` | Catalog sizes. `photo_sizes` counts photos with confirmed sizes. Large tables use the planner estimate and fall back to an exact count when the table has never been analysed. |
| `work_items.<state>` | Work items by state (`pending`, `ready`, `running`, `waiting`, `done`, `failed`). |
| `ready_jobs` | Work items in the `ready` state, i.e. promoted by the scheduler and waiting for a worker. |
| `river_jobs.<state>` | River jobs by state. |
| `calls_last_hour`, `calls_last_10min` | Granted Flickr calls from `api_call_log`. |
| `keys.key_<id>.{calls_last_hour,used,remaining,limit}` | Hourly usage per key against `per_hour_limit`. Key labels and secrets are never logged. |
| `pending_items`, `oldest_pending_age` | Pending work items and the age of the oldest. |
| `failed_work_items`, `dead_jobs` | Failed work items and discarded River jobs (the dead-letter count). |
| `credential_decryption_failures` | Work items whose last error indicates a stored credential could not be decrypted, including prior failures and work held for retry. A warning recommends checking configured key IDs and envelope metadata; it never includes key material. |
| `token_status` | Worst Flickr account state: `none`, `ok`, `reconnect_required`, `revoked`. |

### Idle warning

When `pending_items > 0` and `ready_jobs = 0` for 5 minutes the monitor logs
`WARN crawl idle: pending items but no ready jobs` every minute with a `reason`:

- `no_key`: no valid API key, or every pending item has no `key_id`.
- `quota_exhausted`: every valid key has spent its hourly limit.
- `not_before_in_future`: all keyed pending items are scheduled for later.
- `unknown`: none of the above.

## Admin status endpoint

`GET /admin/ops/status` (administrators only) returns the same numbers as JSON;
see `contracts/openapi/paths/ops.yaml`. It also carries `idle_reason` whenever
pending items exist and none is ready, and reports credential decryption work
items as `credential_decryption_failures`.

## OpenObserve

When `OPENOBSERVE_USERNAME` is set, each tick also posts to the shared
OpenObserve (`OPENOBSERVE_URL`, org `OPENOBSERVE_ORG`, stream
`OPENOBSERVE_STREAM`, default `xflickr`):

- one `event = crawl.health` record (flat fields: `pending_items`, `ready_jobs`,
  `calls_last_10min`, `failed_total`, `tokens_revoked`, `token_status`,
  `work_items_<state>`, `river_jobs_<state>`, catalog counts);
- one `event = crawl.health.key` record per key (`key_id`, `used`, `remaining`, `limit`).

Shipping failures are logged as warnings and never stop the crawl.
The develop helper sources its account from private Workspaces configuration;
credentials remain in the ignored `.env.develop` and are not written to logs.
The notification destination and template names are also stored there as
`OPENOBSERVE_ALERT_DESTINATION` (`xflickr_telegram`) and
`OPENOBSERVE_ALERT_TEMPLATE` (`xflickr_telegram_v1`); they are references to
OpenObserve resources, not credentials. `OPENOBSERVE_ALERT_SOURCE_DESTINATION`
names the existing destination copied once when the XFlickr destination is
missing.

### Alerts

Alert definitions live in `deploy/openobserve/alerts/`:

| Alert | Fires when |
|---|---|
| `xflickr_api_5xx` | A Flickr/API log has status 500 or above. |
| `xflickr_container_down` | No container memory metric arrived for 5 minutes. |
| `xflickr_crawl_health_stale` | Either crawl health event has no record newer than 90 seconds in the configured stream. |
| `xflickr_database_down` | The crawl health query reports a database failure. |
| `xflickr_disk_high` | Disk use is above 80%. |
| `xflickr_disk_critical` | Disk use is above 90%. |
| `xflickr_error_rate` | Flickr call errors exceed 5% over 15 minutes. |
| `xflickr_no_calls_pending` | No Flickr call for 15 minutes while work is pending. |
| `xflickr_quota_exhausted` | A key has spent its hourly quota. |
| `xflickr_quota_near_limit` | A key is at or above 90% of its hourly quota. |
| `xflickr_token_revoked` | A connected Flickr token is revoked. |
| `xflickr_work_failed` | New work items failed in the last 15 minutes. |
| `xflickr_credential_decryption_failed` | Stored credential decryption failures are present. |
| `xflickr_work_backlog_stale` | Pending work is older than 6 hours. |
| `xflickr_scheduler_errors` | More than three scheduler errors in 15 minutes. |
| `xflickr_latency_p95_high` | Flickr call p95 latency exceeds 8 seconds. |
| `xflickr_rate_limited` | Flickr returns a 429 rate-limit response. |
| `xflickr_auth_failed` | Flickr returns 401 or 403. |
| `xflickr_telemetry_ship_failed` | Crawl health cannot ship to OpenObserve. |

Apply them with `docker/scripts/obs-provision.sh [env-file]` (defaults to
`.env.develop`). The script creates the configured metrics stream when needed,
then creates or updates the `xflickr_telegram_v1` template, the
`xflickr_telegram` destination, and all 19 alerts idempotently.
The destination is cloned from `OPENOBSERVE_ALERT_SOURCE_DESTINATION` only when
missing.

## Smoke check

`docker/scripts/develop-smoke.sh` verifies that `/admin/ops/status` is protected,
that a `crawl health` line is in the container log, and that `crawl.health` and
`crawl.health.key` events with their documented fields arrived within 90 seconds
in the configured stream. It also verifies that the enabled freshness alert
targets the configured destination. Set
`XFLICKR_ADMIN_COOKIE` to an admin session cookie to also read the JSON report.
