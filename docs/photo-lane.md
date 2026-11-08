# Photo lane selection

Photos waiting for `getSizes` need no backlog rows. `photolane.Next(n)` (the
`PhotoLane.Next` use case in `schedule/app`) reads the catalog directly: photos
whose sizes are not terminally confirmed for their current `last_update` and
that are not tombstoned, joined with `crawl_interests` through the owner. Only
starred or actively selected contacts are eligible for background size work;
unselected, unstarred contacts (including expired selections) do not enter the
lane, regardless of depth. Explicit photo requests for download or sync use the
existing download flow, which queues `photo.sizes` directly with the requesting
user's key and the interactive quota class. They do not require a starred or
selected owner. A photo being opened in the Lightbox screen (JOOXF-171) will
enqueue an interactive size confirmation through the same download flow.
Listing URLs and dimensions remain provisional until needed.
Once sizes are current, enabled optional facets may be selected for photos owned by
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

Before making any Flickr call, the `photo.sizes` handler checks the stored
listing server. Server `65535` is Flickr's restricted placeholder: anonymous
`getSizes` is never spent on it. The handler instead retries once with the
OAuth token of a connected account that may view the photo (the owner's
connected account first). Background photo-lane jobs only use that owner
account. Interactive on-demand jobs may also try the requesting user's other
accounts. A successful signed call stores `confirmed` sizes through the usual
writer. A failure records an `entity_facet_restrictions` row for that account
only and stores `unavailable`, so the same account is not asked again until the
photo version or visibility reopens the restriction. When no eligible account
remains, the handler stores empty sizes as `unavailable` without further calls.
Repeated jobs skip the terminal status without calls, including interactive
download jobs. An unchanged confirmed result is reused without
reclassification. Cleanup of already queued work is tracked separately in
JOOXF-311.

For other pending photos, the `photo.sizes` handler calls
`flickr.photos.getSizes` without a token first. A confirmed result is stored
and `sizes_checked_at` is recorded. A confirmed photo is skipped while its last
update is no newer than that check. When the anonymous call fails as not-found
or permission denied (including Flickr code 1 on restricted media), the same
authenticated retry rule applies: one signed attempt through the quota governor
and call log, then a per-account restriction on failure. Other terminal Flickr
errors still store empty sizes as `unavailable` without an authenticated retry.

Quota exhaustion returns a River snooze, so it does not consume an attempt and
the same job resumes through the quota governor when its wait expires. Other
transient Flickr, transport, and storage failures use the configured River
retry and backoff policy. If those attempts are exhausted, the job is
discarded and reported through the dead-job outbox; the photo itself remains
unconfirmed in the catalog. The next ordinary photo-lane pump can select it
again, and the new job still goes through key selection and the quota governor.
This makes a discarded job visible and recoverable without a manual requeue or
a quota bypass. Flickr error code 1 (`Photo not found`) is treated as an
unavailable photo by this lane; code 2 (`Permission denied`) is classified
separately and also stored as unavailable. Flickr documents that `getSizes`
does not require OAuth, but the caller must be allowed to view the photo; code
1 can also occur for existing restricted photos, as verified in JOOXF-275,
while code 2 means the caller cannot view it. A sizes failure alone does not
prove that the photo was deleted.
The API client tests both classifications and the FakeFlickr integration test
verifies the requested method, photo ID, retry, and confirmed result. See the
[Flickr `getSizes` API reference](https://www.flickr.com/services/api/flickr.photos.getSizes.html).

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

`catalog/adapters/postgres/photolane.sql` ranks persons from two indexed,
`LIMIT`ed streams (selected, starred) and probes only the
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

## Historical baseline before on-demand filtering (5M photos, 50k people and interests)

Warm calls took 20-31 ms (cold first call about 220 ms); `EXPLAIN (ANALYZE)`
reported 30.9 ms execution, probing 2 persons through the photo index.
