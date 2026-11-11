# Photo lane selection

Photos waiting for `getSizes` need no backlog rows. `photolane.Next(n)` (the
`PhotoLane.Next` use case in `schedule/app`) reads the catalog directly: photos
whose sizes are not terminally confirmed for their current `last_update` and
that are not tombstoned, joined with `crawl_interests` through the owner. Only
starred or actively selected contacts are eligible for background size work;
unselected, unstarred contacts (including expired selections) do not enter the
lane, regardless of depth. Legacy `unavailable` results and photos whose server
is `65535` remain eligible even when their stored check time matches the photo
version, so existing restricted photos can be confirmed through OAuth. Explicit
photo requests for download or sync use the
existing download flow, which queues `photo.sizes` directly with the requesting
user's key and the interactive quota class. They do not require a starred or
selected owner. A photo being opened in the Lightbox screen (JOOXF-171) will
enqueue an interactive size confirmation through the same download flow.
Listing URLs and dimensions remain provisional until needed. Once a current
global result or an eligible photo-owner account result confirms sizes, enabled
optional facets may be selected for photos owned by starred or actively selected
contacts. Account-scoped confirmation can unlock optional work while the
catalog's global `sizes_status` remains provisional.

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

For each job, the handler resolves the XFlickr user who owns its API key and
checks that user's restriction for the current photo version. It reuses a
global confirmed result only while the photo is public and does not use server
`65535`. A non-public photo or restricted placeholder skips anonymous
`getSizes`; other pending photos try it first. A successful anonymous result
is safe to store globally. A not-found or permission response records a
restriction for that XFlickr user and API key; another user or key can still
make its own anonymous attempt.

Restricted photos use at most one signed attempt per job. Candidate accounts
must belong to the requesting XFlickr user, be active, and use a valid API key.
The matching photo-owner account is preferred; interactive requests may also
use the requester's other connected accounts. A successful OAuth result is
stored in `photo_account_sizes` with the exact photo version and visibility.
Catalog and download reads return those URLs only to that account's XFlickr
user while the account remains active and the photo version and visibility
match. Sharing a photo observation does not share its OAuth-authorized sizes.

A failed signed attempt records its restriction by account ID and stores an
account-scoped `unavailable` result without resolving waiting downloads yet.
The same job continues through the remaining eligible accounts owned by the
requester. Only after all candidates fail or are already restricted are that
user's waiting downloads resolved as unavailable; the catalog photo is not
marked globally unavailable. Only a current result from an active, valid account
for the photo owner's Flickr identity closes the background sizes lane; an
outcome belonging to another user's account does not suppress that owner's
work. A current owner-account confirmation also makes eligible optional facets
available. Other accounts' stored sizes remain private to their XFlickr user.
Existing global outcomes remain intact; the schema migration adds account-scoped
storage without rewriting photo rows.
An unchanged anonymous confirmed result is reused without reclassification as
long as the photo remains public and is not a restricted placeholder. Cleanup
of already queued work is tracked separately in JOOXF-311.

Quota exhaustion returns a River snooze, so it does not consume an attempt and
the same job resumes through the quota governor when its wait expires. Other
transient Flickr, transport, and storage failures use the configured River
retry and backoff policy. If those attempts are exhausted, the job is
discarded and reported through the dead-job outbox; the photo itself remains
unconfirmed in the catalog. The next ordinary photo-lane pump can select it
again, and the new job still goes through key selection and the quota governor.
This makes a discarded job visible and recoverable without a manual requeue or
a quota bypass. Flickr error code 1 (`Photo not found`) and code 2
(`Permission denied`) restrict only the requesting key or signed account for
this photo version; neither makes the catalog photo globally unavailable.
Flickr documents that `getSizes` does not require OAuth, but the caller must be
allowed to view the photo. Code 1 can also occur for existing restricted photos,
as verified in JOOXF-275, so a sizes failure alone does not prove deletion.
The API client tests both classifications and the FakeFlickr integration test
verifies the requested method, photo ID, retry, and confirmed result. See the
[Flickr `getSizes` API reference](https://www.flickr.com/services/api/flickr.photos.getSizes.html).

Optional facets are configured by application settings: `photo_info_enabled`,
`photo_exif_enabled`, and `photo_contexts_enabled` default to true;
`photo_comments_enabled` and `photo_favorites_enabled` default to false. They
are app-level settings and cannot be overridden by an individual user. Each
optional job has a separate `photo.facet` kind and runs on `sync-low`; size jobs
remain on `sync`, so optional work has its own worker capacity. A photo cannot
receive optional work until a current global or eligible photo-owner account
size result has been confirmed. Flickr
not-found and permission errors persist an empty result for that photo version;
transient failures retry. Comments and favorites fetch up to 200 pages of 50
items per request, with each request charged to the photo quota.

Optional response data is stored in `photo_info`, `photo_exif`,
`photo_contexts`, `photo_comments`, and `photo_favorites`; `photo_facet_status`
tracks the Flickr photo version completed for each facet. Updates replace the
stored facet and status in one database transaction. Flickr response secrets
are removed before info or EXIF data is persisted.

The current size-confirmation rule lives in the shared SQL fragment
`catalog/adapters/postgres/photo_sizes_confirmed.sql`. Both optional-facet
selection and the worker's source check use it, so an eligible owner-account
result can unlock a facet while global sizes remain provisional.

## Query and indexes

`catalog/adapters/postgres/photolane.sql` ranks persons from two indexed,
`LIMIT`ed streams (selected, starred) and probes only the top-ranked persons.
Each owner contributes at most 50 live photos to one selection. The query
applies that limit before checking account-scoped size results and optional
facet status, using `photos_owner_live_idx` to read each bounded page in upload
order. A current, valid owner account supplies its key for background size
jobs, even when another user's interest ranks that contact first. Restricted
placeholders without a valid owner account are excluded from the background
lane. Other size jobs continue to use the key pool.

A per-owner keyset cursor in `schedule_meta` stores the last upload time and
photo ID. It advances only after that owner's current page has no eligible
jobs, independently of whether more people remain outside the current probe.
When the cursor reaches the end, selection wraps to the newest page. This lets
the lane move past photos already processed by an account or facet without
repeatedly scanning the owner's full catalog.

The account-scoped result primary key `(photo_id, account_id)` supports
skipping an existing result for the selected owner account. Partition indexes
are built concurrently. Tombstoned photos remain excluded. The caller starts
with 128 persons and doubles the probe only while it finds too few photos and a
stream was cut by its limit.

Limit: when the best-ranked persons have no pending photos left, the probe
widens until it reaches persons with work, which costs more per call. The query
places all sizes work before optional facet work and keeps enabled facets in a
stable order.

## Historical baseline before on-demand filtering (5M photos, 50k people and interests)

Warm calls took 20-31 ms (cold first call about 220 ms); `EXPLAIN (ANALYZE)`
reported 30.9 ms execution, probing 2 persons through the photo index.
