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

The row's `starred` and `selected_until` fields support later crawl selection.
This feature does not plan crawl facets or enqueue crawl work.
