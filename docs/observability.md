# Structured logging and telemetry

The application installs a JSON `slog` handler when it starts through
`RunWithHealth`. It writes local logs to stderr and, when OpenObserve username
and URL are configured, forwards logs to OpenTelemetry in batches. Observability
settings load from the optional `.env` file, with process environment values
taking precedence. Traces and metrics use asynchronous OTLP/HTTP exporters as
well. The exporters use
`OPENOBSERVE_URL` as the host, append `/api/<org>/v1/{logs,traces,metrics}`, and
send HTTP Basic authentication plus the configured `stream-name` header.

## Context fields and redaction

`obs.WithFields(ctx, fields)` attaches the supported fields `node`, `role`,
`egress_ip`, `user_id`, `key_id`, and `lane`. The handler adds these fields to
JSON and OpenTelemetry log records. It also adds `trace_id` when the context
contains a valid OpenTelemetry span. Authenticated requests attach the internal
user ID; crawl calls attach their key ID, user ID, and work lane. Callers should
only attach trusted egress IP values, rather than copying untrusted forwarding
headers.

Attribute names containing `secret`, `token`, or `password` are replaced with
`[REDACTED]` in structured records, business events, and log spill output.
Credentials are supplied only in the OTLP Authorization header and are never
included in the configured endpoint URL.

## Business events

Use `obs.Event(ctx, name, attrs)` to write a named structured event and add the
same event to the active trace span. Flickr transport calls emit `flickr.call`
with method, status, latency, response bytes, and classified error code.

The public API-key preflight uses `flickr.test.echo` before an `api_keys` row
exists. It is recorded with a null `key_id`, the signed-in `user_id`, lane
`interactive`, and facet `key_validation`. The request log stores neither the
submitted public key nor the secret. Unassociated calls remain in the overall
call statistics but are excluded from per-key quota usage and hourly rollups.

## Crawl health call counters

The [ops monitor](ops-monitoring.md) reports granted calls using only ledger
rows with `outcome IS NULL`. Completion telemetry (`success`, `failure`,
`throttled`) does not spend a second call. `quota_wait` records are reported as
`quota_waits_last_hour` and `quota_waits_last_10min` in health logs, health
events and admin status, separately from `calls_last_hour`, `calls_last_10min`
and per-key `used`/`remaining`. Existing granted-call field names remain stable
for alerts. Bounded ledger queries and the minute refresh avoid scanning
unbounded history or making any additional Flickr calls.

## Crawl health snapshot cost

The crawl-health snapshot must stay cheap on multi-tens-of-millions photo
tables. Catalog sizes (`people`, `photos`, `favorites`, …) and confirmed
`photo_sizes` use PostgreSQL planner estimates (`pg_class.reltuples`, summed
across partitions, plus `pg_stats` frequencies for confirmed sizes). Exact
`count(*)` over `photos` or `favorites` never runs in the monitor loop.
`counts_estimated` is true on the status JSON and log line when those values
are estimates.

`photo_sizes_pending` is the count of photos matching the partial index
`photos_sizes_pending_idx` (photos that still need sizes work) plus
`photo.sizes` river jobs still waiting (`pending` / `scheduled` /
`retryable`). Queued photos may appear in both terms; the field is a backlog
signal, not a unique photo total. Ready and running counts come only from
`river_job` via the expression index on `(args->>'kind')` for active states.

Only one process runs the snapshot per tick: the monitor wraps the store with
a PostgreSQL session advisory lock (`pg_try_advisory_lock`). Contenders skip
the tick with a debug log. Every snapshot statement runs inside a transaction
with `SET LOCAL statement_timeout` from `ops.health_statement_timeout`
(default 5s, min 1s, max 60s). A timeout marks the snapshot degraded, logs the
query name at warn, and continues; it never stops the crawl. The refresh
interval is `ops.health_interval` (default 60s, min 10s, max 1h).

## Offline buffering

Logs are exported asynchronously with a 10,000-record SDK queue and 512-record
batches. If the OpenObserve request fails, the exporter keeps the newest 10,000
records in memory and retries them with the next batch. Any records still
unsent at shutdown are appended as JSON Lines to
`OPENOBSERVE_SPILL_PATH` (default `var/obs-spill.jsonl`). The spill directory is
created with mode `0750` and the file with mode `0600`. The application does
not replay spill files automatically; they are retained for operator recovery.

The exporter does not perform a startup health check, so a remote OpenObserve
outage does not block process startup. Metrics and traces use the OpenTelemetry
SDK batch/periodic processors; only log records use the 10,000-record fallback
buffer and shutdown spill file.

## Sentry error reporting

When `SENTRY_DSN` is set, every log record at error level or above becomes a
Sentry issue. `SENTRY_ENVIRONMENT` labels the issues, and the release and server
name come from `XFLICKR_VERSION` and `NODE_ID`. A record's attributes are sent
as the `log` context; the attribute named `error` (otherwise the first
error-valued attribute) is reported as the exception so the issue carries a
stack trace. Records below error level are not sent.

The Sentry handler sits behind the same redaction step as the other log
outputs, so secrets and tokens are masked before they reach Sentry. An empty DSN
disables the integration and changes nothing. An invalid DSN is reported once as
a warning without echoing the value, and startup continues. Buffered events are
flushed on `ForceFlush` and on shutdown; both return an error when events were
left unsent, and shutdown also closes the Sentry client. Sentry runs in its own client, with no
global Sentry state.
