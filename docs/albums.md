# Albums

The Albums page lists Flickr album metadata for contacts of the signed-in
user's non-revoked Flickr accounts. The generated client calls authenticated
`GET /me/albums` with a keyset cursor and page size. The API scopes owners
through the user's connected accounts and known contact relationships, and
includes an album only when at least one non-tombstoned album photo is visible
to the signed-in user under the shared photo-visibility policy. It returns no
photo count. Each card shows its cover placeholder, title, owner NSID, and
update date. The API returns the update time and next cursor.

The response also includes the complete `contact_owners` choices, independent
of its album page. The optional `owner_nsid` query parameter filters results on
the server. The visibility query checks at most 128 album photo memberships
per album in one request. If it reaches that bound without finding a visible
photo, the cursor resumes that album's photo probe on the next page.

The page supports loading, empty, error, forbidden, and partial-page states in
English and Vietnamese. Its contact selector uses the complete server-provided
owner choices and asks the server for filtered pages. Pagination loads
additional albums without hiding cards already shown. An empty page with a
cursor means visibility scanning can continue; the page shows its final empty
state only after the cursor is exhausted. Album cards remain static because the
album detail route is not implemented. The empty state has no action because
the Contacts route is not implemented.

Current API limits: cover URLs and owner display names are not returned, so
the list uses a neutral cover and the owner NSID. The page does not refresh from
album events. Album detail, photo browsing, and album downloads remain outside
this implementation. Any future album detail may label only the bounded photo
page as “N photos loaded”; it must not report an album total.
