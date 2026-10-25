# ADR 0003: Durable job queue

- Status: Accepted
- Date: 2026-10-25

## Context

XFlickr needs durable background jobs that are committed atomically with
application changes, can be retried after worker failures, and can be routed to
workers with independent concurrency. Job queue infrastructure must remain
behind a platform port.

## Decision

Use River with its PostgreSQL `pgx` driver as the platform queue adapter. Apply
River's schema through Goose migrations. Enqueue jobs on the caller's
transaction, configure named queues and concurrency on worker processes, and
publish exhausted jobs to the existing outbox under `job.dead`.

## Consequences

- PostgreSQL remains the durable queue store and transaction boundary.
- Queue backend details stay outside feature modules through the `JobQueue`
  port.
- Queue names River cannot accept directly are mapped to stable internal names.
- Capacity based pumping remains the responsibility of scheduling.
