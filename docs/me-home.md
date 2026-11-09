# Home snapshot API

`GET /me/home` returns one snapshot for the authenticated XFlickr user. It
does not call Flickr.

Contact counts use crawl interest depth: depth 1 is direct, depth greater than
1 is discovered, and depth 0 is the user's connected account owner rather than
a contact. Photo counts include only non-tombstoned records visible under the
shared photo policy. A user's own observations may include private photos;
public observations from another user are visible only when that observer has
sharing enabled. Hidden, unobserved records and private records shared by
another user are omitted.

The photo summary reports current confirmed sizes and existing caption
enrichments. New photos are ordered by Flickr upload time descending, then ID
descending, and capped at 12. The first stored Flickr size URL is selected in
thumbnail preference order; photos without a stored size URL return
`thumbnail_url: null`. Starred contacts are ordered by NSID and capped at five.
Their crawl state prioritizes failed work (`attention`), running work
(`crawling`), queued or waiting work (`waiting`), then `idle`.

Activity is an empty array for now. XFlickr publishes crawl and download
updates through the transient SSE channel but does not persist a user-scoped
activity history; the API avoids inventing events from current state. A durable
activity source needs a separate change before this field can contain history.

The repository builds visible photo IDs from the existing observation and
sharing rules, then joins photos by their partition key `id`. Counts are exact
and proportional to the user's observed photos plus records contributed by
sharing users. The response lists remain bounded. Partial indexes support the
five starred contacts and their active crawl work lookups.

The OpenAPI source is `contracts/openapi/paths/home.yaml` and
`contracts/openapi/schemas/home.yaml`. Generated Go server and TypeScript
client artifacts are updated with `make gen`.
