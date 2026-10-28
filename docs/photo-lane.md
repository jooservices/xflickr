# Photo lane selection

Photos waiting for `getSizes` need no backlog rows. `photolane.Next(n)` (the
`PhotoLane.Next` use case in `schedule/app`) reads the catalog directly: photos
whose sizes are not terminally confirmed for their current `last_update` and
that are not tombstoned, joined with `crawl_interests` through the owner. Once
sizes are current, enabled optional facets may be selected for photos owned by
starred or actively selected contacts.

## Ordering

Each interest row ranks as: a live user selection (`selected_until` in the
future, starred first), then a star, then the shallowest depth. A person ranks
by their best interest row across all users. Within a person the newest upload
comes first (missing upload dates last).

## Enqueue

Each selected photo is charged to a key through `KeyPool.Choose` (class
`photo`) and enqueued as one `photo.sizes` job on the `sync` queue. The job
args carry `photo_id`, `owner_nsid`, `facet` (`sizes`), `key_id`, `class`
(`photo`) and `dedupe_key` (`photo:<id>:<facet>`). A photo whose dedupe key is
already in a waiting or running River job is skipped, as is a photo whose owner
has no eligible key. A transaction-scoped advisory lock on the dedupe key keeps
concurrent producers from inserting the same job twice. `Next` pages past
skipped photos (bounded) so already-queued photos do not starve the lane.

The `photo.sizes` handler calls `flickr.photos.getSizes`, stores the returned
sizes as `confirmed`, and records `sizes_checked_at`. A confirmed photo is
skipped while its last update is no newer than that check. Flickr not-found and
permission errors store an empty sizes list with status `unavailable`; those
photos leave the lane and are not retried.

Optional facets are configured by application settings: `photo_info_enabled`,
`photo_exif_enabled`, and `photo_contexts_enabled` default to true;
`photo_comments_enabled` and `photo_favorites_enabled` default to false. They
are app-level settings and cannot be overridden by an individual user. Each
optional job has a separate `photo.facet` kind and runs on `sync-low`; size jobs
remain on `sync`, so optional work has its own worker capacity. A photo cannot
receive optional work until a current size result has been confirmed. Flickr
not-found and permission errors persist an empty result for that photo version;
transient failures retry. Comments and favorites fetch up to 200 pages of 50
items per request, with each request charged to the photo quota.

Optional response data is stored in `photo_info`, `photo_exif`,
`photo_contexts`, `photo_comments`, and `photo_favorites`; `photo_facet_status`
tracks the Flickr photo version completed for each facet. Updates replace the
stored facet and status in one database transaction. Flickr response secrets
are removed before info or EXIF data is persisted.

## Query and indexes

`catalog/adapters/postgres/photolane.sql` ranks persons from three indexed,
`LIMIT`ed streams (selected, starred, other by depth) and probes only the
top-ranked persons through the partial index `photos_sizes_pending_idx`
(`owner_nsid, uploaded_at DESC, id DESC`), which contains photos without a
terminal size result and confirmed photos whose `sizes_checked_at` is older
than `last_update`. Unavailable and tombstoned photos are excluded. The caller
starts with 128 persons and doubles the probe only while it finds too few
photos and a stream was cut by its limit.

Limit: when the best-ranked persons have no pending photos left, the probe
widens until it reaches persons with work, which costs more per call. The query
places all sizes work before optional facet work and keeps enabled facets in a
stable order.

## Measured (5M photos, 50k people and interests, 100 photos per call)

Warm calls took 20-31 ms (cold first call about 220 ms); `EXPLAIN (ANALYZE)`
reported 30.9 ms execution, probing 2 persons through the photo index.
