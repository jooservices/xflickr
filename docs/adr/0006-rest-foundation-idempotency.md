# ADR 0006: Additive REST foundation and durable idempotency claims

- Status: Proposed
- Date: 2026-11-06
- Ticket: JOOXF-316

## Context

The REST migration ships in module slices. Globally changing existing response
writers would break clients before their corresponding modules migrate.
Retry-sensitive creation must survive an API restart and prevent simultaneous
requests from executing the same work, including work that spends Flickr quota.

## Decision

Keep reusable transport helpers in `internal/platform/api/rest` and opt into
them per operation. Its idempotency storage interface is implemented by the
outward PostgreSQL adapter in `rest/postgres`. The additive entry point is
composed in the API role and discovers existing permitted routes until later
slices replace their targets. It does not move existing handlers.

Persist a pending claim under `(principal, key)` before executing work. Bind it
to a fingerprint of the method, request target, media type and exact body.
Completed claims store the original response status, headers and bytes. Replay
those bytes rather than executing a duplicate request. Keep a single 24-hour
retention constant for this slice, as assigned; match the claim generation when
saving so a late worker cannot overwrite a replacement claim.

An unfinished claim returns conflict. Callback/save errors leave it pending
because the operation may already have produced effects. Resource applications
own reconciliation of uncertain work. The foundation does not retry it.

## Alternatives and consequences

An in-memory cache cannot preserve replay safety after restart or coordinate
multiple API processes. Durable claims provide those guarantees but retain
unresolved requests until application reconciliation or expiry.

Holding a transaction around the callback can make database-only effects and
the saved response atomic. It also holds database resources while the callback
runs and cannot roll back external effects such as a provider call. The chosen
claim protocol releases database locks before execution and fails closed on
uncertainty; it does not promise exactly-once external effects after expiry.

Expired entries are atomically replaced on reuse. This slice adds an expiry
index but no background purge or administrator retention control. Those need
follow-up lifecycle policy; the existing table is not an audit archive and
must never store one-time issuance secrets.

See [REST building blocks](../rest-building-blocks.md) for the public helpers,
error semantics and reusable conformance cases.
