# API keys

A user brings their own Flickr API key and secret. XFlickr checks the public
key with one `flickr.test.echo` call and stores the key only when that call
succeeds. A rejected key responds with code `key_invalid` and writes nothing.

The public key is stored in plaintext so workers can call Flickr. Responses
return a masked value: the first 6 characters, then a bullet for each remaining
character. The secret is sealed with platform envelope encryption. The
associated data is the literal `api_keys:<id>` for that row. Endpoints do not
return the secret, and the key check does not send it to Flickr or write it to
the log.

Each key has `per_hour_limit`, `per_second_limit`, and `burst`. Omitted values
on create use the administrator settings. A value cannot exceed `per_second_rate`,
`burst`, or the hourly ceiling. The hourly ceiling is 3,600. When
`daily_call_limit` is a positive number below 3,600, that budget is the hourly
ceiling. Zero means the daily budget is unlimited.

`PUT /keys/{id}` can change the label, the limits, and the secret. It cannot
change the public key. A secret change seals a new value with the same
associated data and sets connected accounts for that key to
`reconnect_required` (see [Flickr accounts](flickr-accounts.md)).

`DELETE /keys/{id}` removes the key and publishes `key.deleted` in the same
transaction. The event carries the key id and user id. Consumers use it to
stop queued work for that key.

A key status is `valid`, `invalid`, or `revoked`. A new key that passes the
Flickr check is `valid`. `valid` may become `invalid` or `revoked`. `invalid`
may become `valid` or `revoked`. `revoked` is terminal, and a revoked key
cannot be updated.

The key routes require a signed-in user. One user cannot read or change
another user's keys. The same user cannot store the same public key twice.
