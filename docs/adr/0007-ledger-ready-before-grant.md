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

Only `restore.lua` writes (creates) the ready marker. `acquire.lua` refuses
with a retryable ledger-not-ready result while it is absent, and on a
successful grant only extends an already-existing marker TTL via `PEXPIRE`
(which is a no-op when the key is missing, so a grant can never create the
marker). The marker lives as long as the ledger is live: restore sets it,
grants extend it, Redis loss removes it. Every Governor is constructed
through one factory that requires a Rebuilder. On refuse, Acquire takes a
Redis rebuild lock (single-flight across roles), rebuilds from the last hour
of grant rows, then retries.

Because acquire fails closed while the marker is absent, no grant can happen
during a rebuild. The call log is therefore authoritative for that hour:
`restore.lua` deletes the key's rolling lane sorted sets, then replays log
rows with `ZADD NX` and sets ready in one atomic Lua call. The early "already
ready → return 0" exit keeps a second restore a no-op. This supersedes the
"retain existing members" rebuild note in ADR 0005: live `<now>:<seq>` members
and call-log `log:…` members have different names, so retaining both would
double-count.

Trade-off: grants are recorded in the call log right after being granted. A
grant whose log row was not yet written when the marker was lost is not
restored. That window is rare and bounded by log write latency.

## Consequences

No role can grant before the ledger matches the durable call log. Concurrent
roles share one rebuild. Grants no longer hide a missing restore, and steady
traffic keeps the marker alive so restore is not triggered under load. After
marker loss, restore replaces lanes from the call log instead of merging with
orphan live members. See [quota governor](../quota.md).
