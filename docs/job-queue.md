# Durable job queue

The platform queue port stores jobs in PostgreSQL through River. Producers
insert jobs with the same `pgx.Tx` as their other changes, so a rolled-back
transaction never leaves runnable work behind.

## Job kinds and routing

Register an application kind with a handler. Per-kind defaults can choose a
queue, maximum attempts, lease timeout, and retry backoff. An enqueue call can
override those values for an individual job. Worker processes configure the
queues they subscribe to and the maximum concurrency for each queue. Queue names
may be generated per node, such as `crawl@node-1`; the River adapter maps names
that River does not accept directly to stable internal queue names.

## Retries and dead letters

River retries failed jobs using the configured backoff and marks exhausted jobs
as discarded. On the last failed attempt, the adapter publishes a `job.dead`
event to the transactional outbox with the job ID, kind, arguments, queue,
attempt count, and last error. Outbox consumers can process dead letters without
coupling workers to a specific delivery destination.

## Scope

The queue provides durable enqueueing, worker registration, per-queue
concurrency, per-kind retry settings, and dead letter publication. Capacity
based pumping is handled by the scheduling feature.
