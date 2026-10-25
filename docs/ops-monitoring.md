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
pending items exist and none is ready.

## OpenObserve

When `OPENOBSERVE_USERNAME` is set, each tick also posts to the shared
OpenObserve (`OPENOBSERVE_URL`, org `OPENOBSERVE_ORG`, stream
`OPENOBSERVE_STREAM`, default `xflickr`):

- one `event = crawl.health` record (flat fields: `pending_items`, `ready_jobs`,
  `calls_last_10min`, `failed_total`, `tokens_revoked`, `token_status`,
  `work_items_<state>`, `river_jobs_<state>`, catalog counts);
- one `event = crawl.health.key` record per key (`key_id`, `used`, `remaining`, `limit`).

Shipping failures are logged as warnings and never stop the crawl.

### Alerts

Alert definitions live in `deploy/openobserve/alerts/`:

| Alert | Fires when |
|---|---|
| `xflickr-no-api-calls` | `calls_last_10min = 0` while `pending_items > 0` in 3 or more of the last 5 minutes. |
| `xflickr-failed-jobs-growing` | `failed_total` (failed work items + dead jobs) rose within 10 minutes. |
| `xflickr-token-revoked` | `tokens_revoked > 0`. |

Apply them with `docker/scripts/openobserve-alerts.sh [env-file]` (defaults to
`.env.develop`). The alerts have no destination; attach one in the OpenObserve
UI. The script uses the v1 per-stream alert API and has to be re-checked after
an OpenObserve upgrade.

## Smoke check

`docker/scripts/develop-smoke.sh` verifies that `/admin/ops/status` is protected
and that a `crawl health` line is in the container log. Set
`XFLICKR_ADMIN_COOKIE` to an admin session cookie to also read the JSON report.
