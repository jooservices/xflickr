# ADR 0007: Ledger ready before any grant

- Status: Accepted
- Date: 2026-11-07
- Ticket: JOOXF-344

## Context

After deploy or Redis loss the rolling ledger can start empty. JOOXF-343's
acquire script set the ready marker on every grant, so a role without a
rebuilder (crawl) could mark the ledger ready before restore ran and then
over-spend Flickr's hourly allowance on top of grants still in `api_call_log`.

## Decision

Only `restore.lua` writes the ready marker. `acquire.lua` only reads it and
refuses with a retryable ledger-not-ready result while it is absent. Every
Governor is constructed through one factory that requires a Rebuilder. On
refuse, Acquire takes a Redis rebuild lock (single-flight across roles),
rebuilds from the last hour of grant rows, then retries. Restore stays
idempotent via `ZADD NX` and the ready check.

## Consequences

No role can grant before the ledger matches the durable call log. Concurrent
roles share one rebuild. Grants no longer hide a missing restore. See
[quota governor](../quota.md).
