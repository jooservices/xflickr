# Downloads

`POST /downloads` creates a per-user download request from a contact, album, or
tray selection. The catalog supplies candidate photos, and the sharing policy
filters out photos the user cannot see. The response reports the number of
skipped photos.

Each visible photo is stored as a `download_items` row. Items with current,
confirmed sizes are queued as `media.fetch` work on the `media` queue. Other
items wait for a `photo.sizes` job on the `sync` queue, using a valid Flickr key
belonging to the requesting user and the interactive quota class.

Packing and ZIP delivery are outside this feature.
