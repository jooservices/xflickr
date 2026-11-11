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
or the source or shared catalog revision changes. Catalog changes can keep the
cached values available as estimates without queuing a full refresh on every
request; the next refresh is scheduled when the interval expires. Counts are
unavailable when the first refresh has not completed or a permission change
invalidated them.

A sharing-policy change invalidates every cached Home count row in the same
transaction as the setting update and queues one deduplicated refresh job. Until
the row is recomputed, the API returns `photos: null`, null photo totals for
starred contacts, and `counts_stale: true`; it does not expose counts from the
previous sharing policy. A missing or revision-stale row is treated the same
way. Ordinary catalog revision changes do not invalidate counts; their cached
values remain estimates while a refresh is pending.

The sync worker refreshes counts in keyset pages capped by the administrator
setting `home.stats_refresh_batch_size` (250 rows by default; maximum 1,000).
Public, non-tombstoned photo candidates are indexed by observer and updated
when observations or photo visibility change. A global indexed candidate set
contains photos shared by at least one enabled account. Changing sharing
permission queues a bounded, keyset-paginated candidate sync in the same
transaction as the setting update. Home checks the current observation,
sharing permission, and Flickr visibility before applying each 12-photo limit,
so stale revoked candidates cannot consume the result limit or expose a photo.
Permission changes invalidate cached counts and restart an active refresh from
the first page. Ordinary catalog revisions keep keyset progress and staged
totals; refresh publishes them as estimates against the latest revision and
waits for the next interval before another full pass.

The candidate migration installs change triggers before its initial backfill.
It processes at most 500 photo IDs per transaction, locks observation rows
through each page commit, and saves a keyset checkpoint so an interrupted
migration can resume without inserting candidates for observations changed in
the meantime.

The request reads at most 12 newest eligible photos per enabled sharing owner
from the indexed per-user candidate table, then deduplicates and returns at most
12. Disabled sharers are filtered before their candidate rows are visited;
current observation and Flickr visibility checks still run before each page
limit.
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
