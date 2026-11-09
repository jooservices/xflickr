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

Pending work stores a time-invariant `score` sort key used only for ordering
(its absolute value is not a minutes-aware score). The key equals the
historical minutes-aware formula with the shared "now" term cancelled, so
dispatch order stays identical without rewriting rows as they age. Scores are
set at insert and recomputed only when star/select, plan weight, depth/direct
flags, or admin score weights change. A weights fingerprint triggers one
set-based pending rescore in bounded batches, writing only rows with outdated
scores. The direct-action bonus ends when the associated selection expires; a
separate bounded pass updates affected rows using the selection expiry index.
The pump lists active API keys from `api_keys`/`flickr_accounts` and lets the
per-key claim hit the pending score index. Plan resource shares reserve each
plan's fraction of available dispatch capacity, then unused allocation is lent
to plans with backlog.

The pump counts at most the current queue limit for each plan. It probes
pending work through that plan's users and stops after the limit, using the
partial pending backlog index ordered by due time. An empty plan therefore
does not scan another plan's pending rows. The capped availability preserves
`SplitShare` behavior because the scheduler can dispatch no more than that
same limit in one pump.

## Self-heal and requeue of failed work

A row that failed only because its lease expired (`last_error = 'lease
expired'`, an infrastructure fault such as a stopped worker) is not final.
Reclaim returns it to `pending` once it has been failed for `SelfHealAfter`
(15 minutes), with `attempts` reset to 0 and the error cleared. Failures with
any other error stay failed.

`xflickr requeue [--id N[,N...]]` returns failed rows to `pending` with
`attempts` reset: all failed rows without `--id`, otherwise only the listed ids
(`--id` may repeat). Payload and cursor are kept, so no data is lost. The
command only changes the row; the normal pump and quota gate dispatch it, so
requeued work never bypasses quota. It prints the number of rows requeued.
