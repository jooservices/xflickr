# Downloads

`POST /downloads` creates a per-user download request from a contact, album, or
tray selection. The catalog supplies candidate photos, and the sharing policy
filters out photos the user cannot see. The response reports the number of
skipped photos.

Each visible photo is stored as a `download_items` row. Items with current,
confirmed sizes are queued as `media.fetch` work on the `media` queue. Other
items wait for a `photo.sizes` job on the `sync` queue, using a valid Flickr key
belonging to the requesting user and the interactive quota class.

Once every item is ready or failed, a `media.pack` job streams ready originals
into ZIP parts using the stored method. Each part is capped at 2 GiB and stored
in the configured blob store. The final part contains `manifest.csv`, with a
row for each photo, its SHA-256 checksum, and any download failure. The download
is marked `ready` or `ready_with_errors` after part metadata is committed.

Part metadata is recorded in `download_parts` with the expiry from the user's
plan ZIP TTL. Original per-photo blobs are removed after the parts are
recorded; the ZIP parts remain temporary.

Owners retrieve short-lived links with `GET /downloads/{id}/parts`. Each link
expires after `DOWNLOAD_LINK_TTL` (15 minutes by default), capped by the ZIP
part's own expiry. A client calls
`POST /downloads/{id}/parts/{partNumber}/fetched` only after that transfer
completes. The media expiry worker deletes the temporary ZIP parts and marks
the download `expired` after every part is acknowledged or the download/part
TTL expires. It keeps a part's database row when deleting its blob fails, so a
later cleanup pass can retry.

`POST /downloads/{id}/retry-failed` atomically resets and requeues only items
still marked failed. Repeating the request is safe. The package is unavailable
while the retry is running and is rebuilt after the retried items settle.
