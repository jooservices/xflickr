# Crawl interests

`crawl_interests` records one row for each XFlickr user and Flickr person. The
pair is the primary key, so users can independently discover the same person at
different depths. Adding another discovery path keeps the lowest depth and the
path that established it. Discoveries deeper than the user's effective
`max_depth` are ignored.

A person remains expandable while at least one interested user has a stored
depth below that user's effective `max_depth`. Connecting or reconnecting a
Flickr account adds its owner as a depth 0 interest. The `crawl` role consumes
`flickr_account.connected` from the transactional outbox; repeated delivery is
safe because interest insertion is idempotent.

Depth 1 follows contacts of the user's connected Flickr accounts. The
`contacts` facet pages authenticated `flickr.contacts.getList` for each
connected account subject, stores the relationships, and reports each contact
as a discovery with `via=contact`. The engine records that interest at
`depth + 1` only when it is within the user's effective maximum depth. Further
depths still come from owners of photos favorited at the prior level. The
admin setting `contacts_refresh_interval` (default 24 hours, minimum 1 hour,
maximum 30 days) bounds how often the same account's contact list is
re-requested.

The authenticated crawl API reports contact counts by depth, work progress by
facet, and active work by person or sizes lane. A user can star or select one of
their contacts and pause or resume its crawl work. Selection raises direct
action priority for seven days. Star and selection changes invalidate scores
for that user's active backlog rows; completed work remains completed. Paused
contacts are omitted from scheduler dispatch until resumed.
