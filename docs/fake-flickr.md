# Fake Flickr server

`backend/cmd/fakeflickr` runs a local Flickr-compatible REST and OAuth service
with generated JPEGs and a CDN-style image path. The built-in YAML seed is
embedded in the binary; `FAKEFLICKR_SEED` can point to another YAML dataset.
The seed includes people, photos, favorites, albums, contacts, groups, and
galleries, with a three-level contact and favorite discovery graph.

`galleries.getList` follows Flickr's continuation contract. It requires the
initial `continuation=0`, returns an opaque numeric offset when more seeded
galleries remain, and returns `-1` on the last page. Send the returned value as
the next request's `continuation`; the gallery list response includes that
cursor alongside its page metadata.

The default listener is `:8088` and can be changed with `FAKEFLICKR_ADDR`.
REST calls use `/services/rest`; OAuth uses
`/services/oauth/{request_token,authorize,access_token}`; image URLs use
`/images/<photo-id>.jpg`.

Set scenarios at startup with `FAKEFLICKR_QUOTA_PER_HOUR`,
`FAKEFLICKR_RATE_LIMIT_AFTER`, `FAKEFLICKR_BAN_IP`,
`FAKEFLICKR_LATENCY_MS`, `FAKEFLICKR_REVOKED_TOKEN`, and `FAKEFLICKR_INVALID_KEY`
(answers Flickr error 100, "Invalid API Key", for that `api_key`). IP bans match the
`X-Client-IP` request header. At runtime, POST JSON to `/__admin/scenarios` with
the corresponding fields (`quota_per_hour`, `rate_limit_after`, `ban_ips`,
`latency_ms`, `revoked_tokens`, `invalid_keys`, and `sizes_need_token`); applying a
new configuration resets per-key call counters. Photos seeded with
`restricted: true` list with server `65535` and answer `photos.getSizes` with
Flickr code 1 unless an `oauth_token` is present; `sizes_need_token` applies the
same getSizes rule to ordinary photo IDs for tests.

`make gen` generates the embedded JPEG from fixed dimensions, colors, and
shapes, producing reproducible image bytes. No real Flickr API credentials are
needed.
