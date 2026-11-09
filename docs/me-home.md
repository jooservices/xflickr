# Home snapshot API

`GET /me/home` returns one snapshot for the authenticated XFlickr user. It
does not call Flickr.

Contact counts use crawl interest depth: depth 1 is direct, depth greater than
1 is discovered, and depth 0 is the user's connected account owner rather than
a contact. Contact counts come from the user's small `crawl_interests` set.

Photo totals, confirmed sizes, and captions come from `user_home_stats`; they
are never counted in the request path. `counts_computed_at` is null until the
first refresh. `estimated` is true when counts are absent or older than the
administrator setting `home.stats_refresh_interval` (900 seconds by default).
An absent or stale snapshot enqueues one unique `home.stats.refresh` job and
returns cached values immediately (zero when no snapshot exists).

The sync worker refreshes counts in pages of at most 250 observed photo IDs.
Each page uses keyset pagination and a short transaction. It joins photos by
the partition key, counts only public, non-tombstoned photos, and applies the
same observation/sharing rule as the endpoint. Counts and per-contact totals
are staged and replaced after the final page, so requests do not see partial
refreshes. A refresh walks the user's observed IDs plus the shared observation
pool; its total work scales with the visible catalogue. This work runs outside
the request path and holds no table-wide locks.

The request reads at most 12 newest public, non-tombstoned photos from the
partitioned `photos` table's upload-time index. Visibility checks use the
observation lookup index and the sharing setting primary key. Starred contacts
are capped at five before joining their cached per-contact totals; active
crawl state uses indexed work-item lookups. No request query counts the
`photos`, `favorites`, or `observations` tables.

The newest photos are ordered by Flickr upload time descending, then ID
descending. The first stored Flickr size URL is selected in thumbnail
preference order; photos without a stored size URL return
`thumbnail_url: null`. Starred contacts are ordered by NSID. Their crawl state
prioritizes failed work (`attention`), running work (`crawling`), queued or
waiting work (`waiting`), then `idle`.

Activity is an empty array for now. XFlickr publishes crawl and download
updates through the transient SSE channel but does not persist a user-scoped
activity history; the API avoids inventing events from current state. A durable
activity source needs a separate change before this field can contain history.

The OpenAPI source is `contracts/openapi/paths/home.yaml` and
`contracts/openapi/schemas/home.yaml`. Generated Go server and TypeScript
client artifacts are updated with `make gen`.
