# Albums

The Albums page lists Flickr album metadata for contacts of the signed-in
user's non-revoked Flickr accounts. The generated client calls authenticated
`GET /me/albums` with a keyset cursor and page size. The API scopes owners
through the user's connected accounts and known contact relationships; it
returns album id, title, owner NSID, Flickr's public photo total, update time,
and the next cursor.

The page supports loading, empty, error, forbidden, and partial-page states in
English and Vietnamese. Its contact selector filters albums already loaded in
the browser, and pagination loads additional albums. Album cards remain static
because the album detail route is not implemented. The empty state has no
action because the Contacts route is not implemented.

Current API limits: card covers, owner display names, and stored-photo counts
are not returned. The total is Flickr's album photo count, not a count of
photos stored by XFlickr. Contact filtering is client-side, and the page does
not refresh from album events. Album detail, photo browsing, and album
downloads remain outside this implementation.
