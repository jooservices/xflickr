# Schedule backlog

The scheduler stores one durable work item per unit of work, such as one
person facet. The dedupe key is `(kind, subject_key, user_id)`; a null user is
treated as a single shared scope. Work items carry a compact JSON cursor for
continuing paginated work, along with priority, retry count, due time, and the
last error.

New work starts in `pending`. The lifecycle is `pending -> ready -> running`,
then `running -> waiting`, `done`, or `failed`. A completed item remains
deduplicated unless it is explicitly enqueued with `reopen=true`. Advancing a
running item stores its next page cursor and returns it to `pending`, so the
next page is not generated before the current page finishes.

The scheduler pump runs every two seconds under a PostgreSQL advisory leader
lock. It asks the Redis quota governor for each API key's remaining two-minute
capacity, then applies per-key, node, and worker type ready caps. It selects
only the bounded number of due rows from the pending score index with
`FOR UPDATE SKIP LOCKED`. River job insertion and each row's transition to
`ready` share one transaction.

Ready rows receive a lease expiry. An expired lease returns the row to pending,
increments its attempt count, and schedules exponential backoff beginning at
the configured retry delay. Exhausted rows become failed and publish
`job.dead` through the transactional outbox. If Redis cannot report capacity,
the pump does not enqueue work for that tick.
