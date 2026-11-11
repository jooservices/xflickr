# Sharing settings and observations

Each catalog observation records the XFlickr user, connected Flickr account,
record kind, record ID, and first seen time. Repeated observations of the same
record for that account do not change the original timestamp. Catalog upserts
can write observations through the catalog observer port in the same database
transaction.

Sharing defaults to off for every user. `GET /api/v1/me/sharing` returns the
current choice in the standard REST envelope. `PUT /api/v1/me/sharing` replaces
that choice and publishes `sharing.changed` only when the value changes. The
outbox event and setting update commit together. Both responses are private and
not stored by caches; errors use `application/problem+json`.

See [the sharing and privacy rules](SPEC.md#sharing-and-privacy).

The crawl composition root now supplies that writer for every facet and pins the
account used to sign the call. See [crawl observations and historical recovery](crawl-observations.md)
for the single inventory of observed record kinds, transaction/replay behavior,
and the explicitly guarded historical backfill. Backfilled timestamps are
approximate; sharing and photo visibility rules remain unchanged.
