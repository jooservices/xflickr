# Contact screens

The Contacts screen lists only the signed-in user's crawl interests. It uses
`GET /me/crawl/interests` with server-side search, filtering, sort-bound cursor
pagination, and counts for the available filters. The server derives the actor
from the session. A Flickr NSID is the contact identifier; there is no separate
numeric contact ID.

Contact list and detail reads use `crawl_interests` for ownership and summary
progress. Photo totals come from `people.photos_count`; per-user crawled photo
and favorite counts are updated in the crawl page transaction. Size readiness
is advanced from confirmed sizes for photos the user observed. These screens do
not count the large `photos` or `work_items` tables to render a request.

Recently updated thumbnails are limited to three per contact and follow the
same observation and sharing predicate as catalog photo reads. Contact photo
and favorite pages are cursor-paginated and apply that visibility predicate to
every item. Albums, galleries, groups, and About data are limited to 100 rows
per section. All contact routes require USER access and verify that the contact
is in the actor's interests.

The detail view provides Photos, Favorites, Albums, Galleries, Groups, and
About sections. Actions include star/unstar, prioritise pending photo-size
work, pause/resume contact crawl, per-group pool-crawl preferences, download
all contact photos, and download the contact's favorites. Download
authorization is checked against the signed-in user's observations and sharing
settings. Loading, empty, inaccessible, error, retry, and download dialog
states are covered by component/MSW and mocked Playwright tests.
