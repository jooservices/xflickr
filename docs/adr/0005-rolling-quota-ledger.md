# ADR 0005: Rolling quota grant ledger

- Status: Accepted
- Date: 2026-11-06
- Ticket: JOOXF-343

## Context

A fixed window can release capacity while recently granted calls still belong
in Flickr's preceding 60-minute allowance. Aggregate usage snapshots cannot
recover the expiry time of each grant after Redis loss.

## Decision

Keep one Redis sorted set per key and class (interactive, person, photo).
Score unique grant members with Redis TIME in milliseconds. One Lua acquisition
trims expired members, derives total and lane counts, applies the existing lane
and second gates, and adds a grant atomically. Reset and hourly retry use the
oldest counted member's expiry; person demand expiry can permit an earlier retry.

Rebuild before acquisition when the versioned ready marker is absent. Replay
exact timestamps and classes from durable grant rows (`outcome IS NULL`) in the
last hour after clearing the untrusted lane sets (see ADR 0007). Aggregate
minute snapshots remain history and use `window_start = snapshot time - 60
minutes`.

## Consequences

Recent grants stay charged across hour boundaries. Redis memory grows with the
number of grants in the rolling hour; each active class set has a two-hour TTL.
Flickr quota errors add synthetic grants up to the configured limit and cool
the key until the oldest member expires. All roles must restart together on
first deployment; mixed fixed-window and rolling governors are unsupported.
A full day of live grant-log validation follows deployment.

See [quota governor](../quota.md) for the operational and verification details.
