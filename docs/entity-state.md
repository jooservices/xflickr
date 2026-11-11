# Deleted entities and restricted facets

Flickr failures address the ID in the request, not the records returned by a
list. `user_id` identifies a person, `photoset_id` an album, `gallery_id` a
gallery, and `group_id` a group. A not-found result records a global `gone`
entry in `entity_states`. The first timestamp and stable reason are retained,
including when the missing ID has not yet been inserted into the catalog.
Catalog upserts do not clear this marker. People marked gone are excluded from
cycle scheduling, and workers skip marked subjects before acquiring quota.

Permission denied restricts only the addressed subject, Flickr method, XFlickr
user and API key. Checks and full scans share a restriction with the equivalent
list method. Other methods and callers remain eligible. A skipped change-check
probe advances to its other probe; a blocked or incomplete reconciliation never
applies a missing-record diff. No pending jobs are deleted by this feature.

`entity_facet_restrictions` preserves `first_seen_at` and `reason`. Person
restrictions reopen after `restricted_recheck_interval` seconds (default
2,592,000, 30 days). This application setting is registered for admin defaults
and does not allow user overrides. A failed eligible recheck advances the
restriction deadline while retaining the first outcome. Successful rechecks do
not need to delete the historical row; its deadline has already elapsed.

Photo-addressed not-found and permission failures restrict the individual facet
(`sizes`, `info`, `exif`, `contexts`, `comments`, or `photo_favorites`). For
anonymous sizes failures, the restriction is scoped to the XFlickr user and API
key. For OAuth sizes failures, it is scoped to the connected account ID, so two
accounts that share an API key are evaluated independently. A later request can
try another eligible account owned by that requester. OAuth-authorized sizes
are stored and served only for that account's XFlickr user. Workers do not
tombstone a photo or mark its other facets complete.
Photo restrictions have no time expiry: they reopen when Flickr's `last_update`
advances or when reconciliation changes visibility and publishes
`photo.visibility_changed`. The scheduler checks the chosen caller and current
version before enqueuing, and the worker checks again before a call. In-flight
requests may finish concurrently with a new outcome; existing queued work is
retained and checked at execution.

Photos continue to use reconciliation's `tombstoned_at` as their deleted marker.
Photo workers and scheduling reject tombstones and photos owned by gone people.
The existing requirement that optional facets wait for confirmed sizes remains
in effect. No live Flickr call is needed to verify this behavior: unit fakes and
worktree PostgreSQL integration tests cover classification, caller isolation,
first-write preservation, interval/version reopening and visibility changes.

Legacy `sizes_status=unavailable` rows predate caller attribution and remain
unchanged by the account-scoped sizes migration. New failures use
caller-specific restriction rows; they do not complete global photo sizes.
