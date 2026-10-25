# Crawl engine

One engine runs every crawl facet. A facet implements
`crawl/domain.Facet` (`Name`, `Lane`, `Plan`, `Handle`) and is added to the
`Registry`; no engine change is needed. One work item is one Flickr method
paged to its end, and one job is one page.

## Job flow

The scheduler pump dispatches a ready work item as a job whose arguments carry
`work_item_id`. `Engine.Handle` then:

1. loads the item and skips it when it is already `done` or `failed`;
2. marks it `running` (a rerun after a crash finds it still running);
3. asks the key pool for the key that pays, then takes a governor permit for
   the facet's lane;
4. calls Flickr with the item's cursor (`page`, `method`, `params`) and
   releases the permit with the call outcome;
5. passes the payload to `Facet.Handle`;
6. in one unit of work: locks the item, upserts catalog records, records
   observations, adds an interest one level deeper (`depth + 1`, ignored beyond
   the user's maximum depth) for each discovery, creates the planned and
   returned work items, then advances the cursor (`HasMore`) or finishes the
   item.

The commit only applies while the item is still `running` on the page that was
fetched. A page that was already committed, or an item that finished, changes
nothing. Observations are unique per item, page, kind and subject. A crash
between the call and the commit rolls everything back and the rerun repeats the
page.

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

## Photos and favorites facets

The `photos` facet pages `flickr.people.getPhotos`; `favorites` pages
`flickr.favorites.getList`. Both request up to 500 photos with Flickr's URL
size extras. The page commit upserts the photos and records the returned URL
sizes as provisional. The favorites facet also upserts each photo owner and
reports that owner as a discovery. The engine records that interest at
`depth + 1` only when it is within that user's effective maximum depth. Photos
on a maximum-depth page are still stored; their owners do not create deeper
interests.

## Change checks and incremental work

The `check` facet probes a person's photo and favorite lists with
`per_page=1`, using that user's stored upload and favorite checkpoints. A
positive Flickr `photos.total` produces only the matching `photos` or
`favorites` work item, carrying the same minimum date and the normal 500-item
page size. A zero total produces no incremental work. Finishing that work
advances its corresponding checkpoint. Completed or failed incremental items
reopen with the new cursor; pending and running items keep their current work.
The settings interval defaults to 24 hours when no positive effective interval
is supplied.

`crawl_cycles` stores each user's cursor through the full NSID-ordered list.
The cursor advances over people who are not yet due without marking them
checked, and wraps only after visiting the end of the list. At a wrap, one
`flickr.photos.getContactsPhotos` probe is scheduled per connected account.
The `contacts_photos` facet queues a check for each distinct owner in the
response. `crawl_interests` stores per-person photo, favorite, and check
checkpoints. The settings interval is enforced by the scheduler, not a cron
job.
