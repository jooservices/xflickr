# Home snapshot API

`GET /api/v1/me/home` returns a private snapshot for the authenticated user.
This first slice provides `generated_at`, contact counts, and nullable photo
metrics. Newest photos, starred contacts, and activity have no response fields.

## Contact counts

Counts come from that user's `crawl_interests` rows. Depth 1 is direct; depths
greater than 1 are discovered. Depth 0 is the user's own Flickr account and is
excluded. `total` is the sum of direct and discovered contacts. The query is
scoped by `user_id` and groups the user's interest rows by depth.

The count is exact at query time. Work scales with the number of interests for
the current user; there is no per-user row cap in this slice. The endpoint does
not query `work_items` or call Flickr.

## Unavailable sections

The response names `photos`, `starred_contacts`, and `activity` in
`unavailable_sections`. `photos.total`, `photos.sizes_ready`, and
`photos.captioned` are `null` until JOOXF-391 provides stored statistics.
Newest photos, starred contacts, and activity fields are omitted; the API does
not report fabricated zero counts or empty collections. Those features will be
added in later slices.

Responses use the standard REST envelope, include `links.self`, and set
`Cache-Control: private, no-store`.
