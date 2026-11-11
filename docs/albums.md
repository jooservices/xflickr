# Albums

The Albums page lists Flickr album metadata for contacts of the signed-in
user's non-revoked Flickr accounts. The generated client calls authenticated
`GET /me/albums` with a keyset cursor and page size. The API scopes owners
through the user's connected accounts and known contact relationships. It
filters albums by their owner's contact relationship and does not inspect
album photos or observations. Each card shows its cover placeholder, title,
owner NSID, and update date; it has no photo count. The API returns the update
time and next cursor.

The page supports loading, empty, error, forbidden, and partial-page states in
English and Vietnamese. Its contact selector filters albums already loaded in
the browser, and pagination loads additional albums. Album cards remain static
because the album detail route is not implemented. The empty state has no
action because the Contacts route is not implemented.

Current API limits: cover URLs and owner display names are not returned, so
the list uses a neutral cover and the owner NSID. Contact filtering is
client-side, and the page does not refresh from album events. Album detail,
photo browsing, and album downloads remain outside this implementation. Any
future album detail may label only the bounded photo page as “N photos loaded”;
it must not report an album total.
