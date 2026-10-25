# Sharing settings and observations

Each catalog observation records the XFlickr user, connected Flickr account,
record kind, record ID, and first seen time. Repeated observations of the same
record for that account do not change the original timestamp. Catalog upserts
can write observations through the catalog observer port in the same database
transaction.

Sharing defaults to off for every user. `PUT /me/sharing` updates the current
user's choice and publishes `sharing.changed` only when the value changes. The
outbox event and setting update commit together.

See [the sharing and privacy rules](SPEC.md#sharing-and-privacy).
