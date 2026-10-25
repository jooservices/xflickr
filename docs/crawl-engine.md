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
