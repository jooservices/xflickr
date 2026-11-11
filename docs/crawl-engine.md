# Crawl engine

One engine runs every crawl facet. A facet implements `crawl/domain.Facet`
(`Name`, `Lane`, `Plan`, `Handle`) and is added to the `Registry`; no engine
change is needed. One work item is one Flickr method paged to its end, and one
job is one page. A work item always belongs to a user.

## Job flow

The scheduler pump dispatches a ready work item as a job whose arguments carry
`work_item_id`. `Engine.Handle` then:

1. loads the item and skips it when it is already `done` or `failed`;
2. marks it `running` (a rerun after a crash finds it still running);
3. asks the key pool for the key that pays, then takes a governor permit for
   the facet's lane;
4. calls Flickr with the item's cursor (`page`, `continuation`, `method`,
   `params`) and
   releases the permit with the call outcome;
5. passes the payload to `Facet.Handle`;
6. in one unit of work: locks the item, ensures people exist, writes catalog
   records that reference them, records observations, adds an interest one
   level deeper (`depth + 1`, ignored beyond the user's maximum depth) for each
   discovery, creates the planned and returned work items, applies a complete
   reconciliation diff when marked by the final page, refreshes only changed
   people last, then advances the cursor (`HasMore`) or finishes the item.
   A nonempty `Result.NextCursor` stores an opaque remote continuation in the
   same cursor JSON as the page number; an empty value keeps the page-counter
   behavior.

The commit only applies while the item is still `running` on the page that was
fetched. A page that was already committed, or an item that finished, changes
nothing. Observations are unique per item, page, kind and subject. A crash
between the call and the commit rolls everything back and the rerun repeats the
page.

The late people refresh keeps its row locks until the page transaction commits,
so it runs after the other writes and locks changed rows in NSID order. The
earlier ensure step is ordered by NSID and does not update or row-lock existing
people.

## Error outcomes

| Error | Outcome |
|---|---|
| `ErrQuotaExceeded`, no key, no permit | item waits (`pending` with `not_before`), no attempt consumed |
| `ErrNotFound` | item `done` |
| `ErrTokenRevoked` | all active items of the user fail; `crawl.stopped` is published |
| `ErrInvalidKey` | all active items of the key fail; `crawl.stopped` is published |
| `ErrIPBlocked` | reported to node health, item requeued, no attempt consumed |
| anything else | exponential backoff and one attempt consumed; `failed` after the maximum |

New work items are created without a key; assigning one is left to the
scheduler.

## Gallery continuation

`flickr.galleries.getList` requires the first request to send
`continuation=0`. Each response supplies the next `continuation`; the handler
stores that opaque value with the page rows before the next request. The
documented terminal value is `-1`. Gallery continuation requests omit the
legacy `page` parameter. If an older response has no continuation field, the
facet uses `pages` and the current page number as before.

A response that repeats the current continuation, or an empty page with a
nonterminal continuation, finishes the scan and logs a warning. It does not
queue another Flickr call. A failure before the page transaction commits leaves
both its rows and the cursor unchanged, so a retry repeats only that
uncommitted page.

## Photos and favorites facets

The `photos` facet pages `flickr.people.getPhotos`; `favorites` pages
`flickr.favorites.getList`. Both request up to 500 photos with Flickr's URL
size extras. The page commit upserts the photos and records the returned URL
sizes as provisional. The favorites facet also stores each favorite
relationship with Flickr's `date_faved`, upserts each photo owner, and reports
that owner as a discovery. The engine records that interest at
`depth + 1` only when it is within that user's effective maximum depth. Photos
on a maximum-depth page are still stored; their owners do not create deeper
interests.

## Scan-start checkpoints (JOOXF-345)

An initial `photos` or `favorites` scan captures a scan-start checkpoint
(`cursor.checkpoint_at`) before the first Flickr page is requested. The pure
domain rule `NextCheckpoint(scanStart, pagesCommitted, old)` is the only place
that decides what to store: a failed or partial scan keeps the previous
interest checkpoint; only when every page of the scan has committed does the
final page transaction write the scan-start time into
`crawl_interests.last_seen_at` (photos) or `last_fave_at` (favorites).

Later checks and incremental pages use those stored bounds as inclusive Flickr
filters:

- photos: `min_upload_date` on
  [flickr.people.getPhotos](https://www.flickr.com/services/api/flickr.people.getPhotos.html)
  (XFlickr does not use `min_taken_date` for crawl checkpoints)
- favorites: `min_fave_date` on
  [flickr.favorites.getList](https://www.flickr.com/services/api/flickr.favorites.getList.html)

Inclusive bounds re-read items exactly at the checkpoint once; the upsert is
idempotent. Late arrivals during a long scan are covered because the stored
value is the start time, not the finish time. Checkpoints are per
`(user_id, person_nsid)` observer scope.

Existing null or zero checkpoints are repaired offline by
`xflickr repair-checkpoints --batch-size 500` (no Flickr calls). Repair uses
only a successful final-page marker and scan-start cursor from the matching
`photos` or `favorites` work item for the same `(user_id, person_nsid)`
interest. The marker is written in the final page transaction; a work item
finished because it was suppressed, restricted, or not found does not receive
it. Shared catalog observation times do not prove scan completion.

For legacy work without a trustworthy completion marker and scan-start
checkpoint, repair sets the checkpoint to Unix second 1 only when a matching
observer-scoped work item is `done`. The old row does not reveal whether it
finished a scan or was skipped, so this conservative catch-up floor can only
cause the next positive change check to read the full list once; it cannot
skip records. A successfully completed catch-up scan replaces the floor with
its real scan-start checkpoint. If the observer has no matching done work
item, the checkpoint stays zero and the report counts it as unrepairable.
Non-zero checkpoints are never overwritten.

Repair locks each bounded interest batch and waits up to 500 ms for row locks.
A lock timeout fails that batch and the command exits without advancing past
the locked row; rerunning the repeatable command retries it. It does not use
`SKIP LOCKED`, which could move the keyset cursor past an interest permanently.

The job advances a keyset cursor across every selected interest, including
rows without completion evidence, and reports those rows separately. Its
completed-work lookup uses the existing unique `work_items` key on
`(kind, subject_key, user_id)`; it does not aggregate or scan the photos or
favorites catalog tables.

## Change checks and incremental work

The `check` facet probes a person's photo and favorite lists with
`per_page=1`, using that user's stored upload and favorite checkpoints. A
positive Flickr `photos.total` produces only the matching `photos` or
`favorites` work item, carrying the same minimum date and the normal 500-item
page size. The final favorite probe also reopens album, gallery, and group
refresh work for the person. These facets call `flickr.photosets.getList`,
`flickr.galleries.getList`, and `flickr.people.getGroups`; page commits upsert
metadata and group membership idempotently. A zero total produces no
incremental photo or favorite work. Finishing that work advances its
corresponding checkpoint. Completed or failed items reopen with the new cursor;
pending and running items keep their current work.
The settings interval defaults to 24 hours when no positive effective interval
is supplied.

## Full catalog reconciliation

The cycle scheduler independently checks `reconcile_interval` (default 30 days)
for each person. A durable `reconcile` item reads every page of the person's
photo list and then every page of favorites using one start-time checkpoint.
Only after the final favorites page commits does the engine tombstone missing
photos and mark missing favorite relationships as removed. A failed or
incomplete scan does not apply a diff. See [Catalog reconciliation](reconciliation.md)
for the privacy and event rules.

`crawl_cycles` stores each user's cursor through the full NSID-ordered list.
The cursor advances over people who are not yet due without marking them
checked, and wraps only after visiting the end of the list. At a wrap, one
`flickr.photos.getContactsPhotos` probe is scheduled per connected account.
The `contacts_photos` facet queues a check for each distinct owner in the
response. `crawl_interests` stores per-person photo, favorite, and check
checkpoints. The settings interval is enforced by the scheduler, not a cron
job.

## Scoring

Work-item priority uses the static sort key described in
[Schedule backlog](schedule.md) and [Database performance notes](database.md).
Star and select controls rescore the person's pending backlog immediately;
there is no periodic aging rewrite.
Crawl inserts and explicit reopens persist the same starred-interest value used
to calculate their static score. For databases with pending rows from an older
build, use the bounded correction in the
[work-items rebuild runbook](runbooks/rebuild-work-items.md#starred-pending-work-correction)
before starting the updated scheduler.

See [Deleted entities and restricted facets](entity-state.md) for global gone
markers, caller-specific restrictions and request suppression.

## Photo sizes backlog in crawl health

The crawl-health field `photo_sizes_pending` counts photos that still need
sizes work through `photos_sizes_pending_idx` (provisional / none / stale
confirmed, not tombstoned) and adds waiting `photo.sizes` river jobs. It is a
backlog signal and may double-count a photo that already has a queued job.
Ready and running size jobs are reported separately. Details and the advisory
lock / statement timeout settings live in
[observability](observability.md#crawl-health-snapshot-cost).
