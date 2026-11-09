# Home snapshot API

`GET /me/home` returns one snapshot for the authenticated XFlickr user. It
does not call Flickr.

Contact counts use crawl interest depth: depth 1 is direct, depth greater than
1 is discovered, and depth 0 is the user's connected account owner rather than
a contact. Contact counts come from the user's small `crawl_interests` set.

Photo totals, confirmed sizes, and captions come from `user_home_stats`; they
are never counted in the request path. `counts_computed_at` is null until the
first refresh. `estimated` is true when counts are absent, older than the
administrator setting `home.stats_refresh_interval` (900 seconds by default),
or their source or sharing revision has changed.
An absent or stale snapshot enqueues one unique `home.stats.refresh` job and
returns cached values immediately (zero when no snapshot exists).

The sync worker refreshes counts in keyset pages capped by the administrator
setting `home.stats_refresh_batch_size` (250 rows by default; maximum 1,000).
Public, non-tombstoned photo candidates are indexed by observer
and updated when observations, photo visibility, or sharing permissions
change. A global indexed candidate set contains photos shared by at least one
enabled account. Home reads and refresh pages union that set with the user's
own candidates, so work stays bounded independently of the number of sharing
accounts. Counts and per-contact totals are staged and replaced after the
final page. A source revision change resets the staged work, and publication
checks both the user and shared revisions.

The request reads at most 12 newest photos from the user's candidate index and
12 from the global shared candidate index before joining photos.
Starred contacts are capped at five before joining their cached per-contact
totals; active crawl state uses indexed work-item lookups. No request query
counts the `photos`, `favorites`, or `observations` tables.

The newest photos are ordered by Flickr upload time descending, then ID
descending. The first stored Flickr size URL is selected in thumbnail
preference order; photos without a stored size URL return
`thumbnail_url: null`. Starred contacts are ordered by NSID. Their crawl state
prioritizes failed work (`attention`), running work (`crawling`), queued or
waiting work (`waiting`), then `idle`.

Activity contains up to five recent user-scoped crawl work items, downloads,
and API call events, ordered by occurrence time. Each source is queried with a
user or key index and bounded before the events are combined.

The OpenAPI source is `contracts/openapi/paths/home.yaml` and
`contracts/openapi/schemas/home.yaml`. Generated Go server and TypeScript
client artifacts are updated with `make gen`.
