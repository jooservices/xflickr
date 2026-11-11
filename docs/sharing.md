# Sharing settings and observations

Each catalog observation records the XFlickr user, connected Flickr account,
record kind, record ID, and first seen time. Repeated observations of the same
record for that account do not change the original timestamp. Catalog upserts
can write observations through the catalog observer port in the same database
transaction.

Sharing defaults to off for every user. `GET /api/v1/me/sharing` returns the
current choice in the standard REST envelope. `PUT /api/v1/me/sharing` replaces
that choice and publishes `sharing.changed` only when the value changes. The
outbox event, setting update, shared Home candidate set, and one durable Home
refresh wake-up per change commit together. Candidate maintenance makes sharing
permission effective immediately; the bounded worker sync remains a recovery
path. Wake-ups are not deduplicated, so a job finishing during the setting
transaction cannot consume the next candidate sync before it commits. Both
responses are private and not stored by caches; errors use
`application/problem+json`.

See [the sharing and privacy rules](SPEC.md#sharing-and-privacy).

The crawl composition root now supplies that writer for every facet and pins the
account used to sign the call. See [crawl observations and historical recovery](crawl-observations.md)
for the single inventory of observed record kinds, transaction/replay behavior,
and the explicitly guarded historical backfill. Backfilled timestamps are
approximate; sharing and photo visibility rules remain unchanged.

Photo visibility reads use `observations` lookups by record ID and kind, then by
user ID for direct access. The existing `observations_p00_kind_record_idx`
supports p00. Migration
`20261111190000_observations_record_lookup_index.sql` builds the matching
`(record_id, record_kind, user_id)` B-tree index concurrently on p01 through p15.
If the migration is interrupted, completed valid leaf indexes are retained on
retry; an invalid concurrent build must be dropped concurrently before retrying.
New observations partitions must receive a lookup index before photo reads
depend on them.
