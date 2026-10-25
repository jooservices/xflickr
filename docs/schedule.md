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

Dispatching pending work into the ready queue is handled separately from this
backlog API.
