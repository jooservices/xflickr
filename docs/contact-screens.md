# Contact screens

The Contacts screen lists only the signed-in user's crawl interests. It uses
`GET /me/crawl/interests` with server-side search, filtering, sort-bound cursor
pagination, and counts for the available filters. The server derives the actor
from the session. A Flickr NSID is the contact identifier; there is no separate
numeric contact ID.

Contact list and detail reads use `crawl_interests` for ownership and summary
progress. Photo totals come from `people.photos_count`; the migration backfills
photo, favorite, and size summaries from each user's observations, plus
contact-wide tombstone counts, in keyset batches. Full photo and
favorite scans store the absolute page position, while incremental pages count
only records not already observed or active. Reconciliation updates removed
favorite and tombstone totals, and a photo that reappears reduces its tombstone
count. Size readiness is updated from confirmed photos in the current bounded
page. These screens do not count the large `photos` or `work_items` tables to
render a request.

Recently updated thumbnails are limited to three per contact and follow the
same observation and sharing predicate as catalog photo reads. Contact photo
and favorite pages are cursor-paginated and apply that visibility predicate to
every item. Their cursors point to the last returned item; the extra row is
used only to determine whether another page exists. Album cover URLs use the
same observation, sharing, and tombstone checks. Albums, galleries, and groups
use keyset pagination with a maximum page size of 100 and report the full
collection total. Album `stored_count` reflects visible stored membership
photos; `total_count` remains Flickr's album count. About data is a fixed
summary. All contact routes require USER access and verify that the contact is
in the actor's interests.

The detail view provides Photos, Favorites, Albums, Galleries, Groups, and
About sections. Actions include star/unstar, select the contact for seven days,
prioritise pending photo-size work in indexed batches, pause/resume contact
crawl, per-group pool-crawl preferences, download all contact photos, and
download the contact's favorites.
Contact and favorite downloads page through the visible photo IDs and submit
requests in batches of at most 500 IDs. Each batch is a separate download entry
and follows the existing observation and sharing checks.
Enabling a group preference queues a `group_pool` work item; the crawl worker
checks the preference before every Flickr request, and a disabled preference
ends pending work. Download
authorization is checked against the signed-in user's observations and sharing
settings. Loading, empty, inaccessible, error, retry, and download dialog
states are covered by component/MSW and mocked Playwright tests.
