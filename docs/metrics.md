# Operations metrics collection

Phase 1 of JOOXF-368 stores deterministic operations metrics for the future
admin dashboard. Collection never calls Flickr. REST and UI arrive in later
phases.

## Module layout

`backend/internal/platform/metrics`:

- `domain/` — `Sample`, catalogue, ports (`Collector`, `Store`, `Clock`,
  `RetentionPolicy`)
- `app/` — `Runner` (per-collector cadence, failure isolation) and
  `RetentionJob` (hourly rollup then bounded purge)
- `adapters/postgres` — idempotent upserts and rollup/purge SQL
- `adapters/process` — CPU, RSS, Go runtime, FDs, uptime, build version,
  HTTP byte counters
- `adapters/river` — River queue depths, worker heartbeat, outbox backlog
- `adapters/growth` — approximate table rows, database size, Redis/ES/media
  when configured

Every role starts the runner with a `role` label. The scheduler also runs the
growth collector (every 10 minutes) and the retention job.

## Catalogue

All metric names, units and descriptions live in
`domain/catalogue.go`. Collectors must emit only catalogue names; the runner
rejects unknown names. Add a catalogue entry before emitting a new metric.

## Cadence

| Collector | Interval | Notes |
|---|---|---|
| process | 1 minute | Per role process |
| river | 1 minute | Queue depths are cluster-wide; heartbeat is per role |
| growth | 10 minutes | Scheduler only; `reltuples` estimates, never `count(*)` on large tables |

## Network bytes

`/proc/self/net/dev` is namespace-wide, so process attribution uses an
in-process `ByteCounter` updated by HTTP server middleware and an HTTP client
`RoundTripper`. Wire those wrappers where request traffic is measured.

## Storage

- `metric_samples` — minute buckets, unique on `(name, labels_hash, bucket)`,
  upsert batches of at most 500 rows
- `metric_rollups` — hourly avg/min/max/p95 written before minute rows are
  deleted

## Retention

Setting `metrics.retention_days` (default 30, min 1, max 365) is seeded in
`app_settings` and editable by administrators. The retention job rolls up
completed hours, then deletes at most 2000 expired minute rows per run so the
crawl is never blocked.

## Overhead

Collection runs in a background goroutine, uses at most one DB connection per
statement, and isolates collector panics/errors into
`metrics.collector_errors`. OpenTelemetry export to OpenObserve remains the
existing `platform/obs` path; this module does not duplicate that export.

## How to add a metric

1. Add an entry to `domain/catalogue.go`.
2. Emit it from the right collector (or add a small collector behind the
   `Collector` port).
3. Cover the behaviour with a unit test; use integration tests when SQL is
   involved.
