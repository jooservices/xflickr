# Feature flags

Feature flags let the team merge a feature before enabling its behavior. Every
unknown flag is off. An enabled flag can target everyone, active administrators,
or a configured set of positive user IDs.

`GET /admin/flags` lists configured flags. `PUT /admin/flags` atomically creates
or updates the supplied flags; flags omitted from the request remain unchanged.
Both routes require an administrator session, and the write route also requires
the normal CSRF token. There is no feature flag UI in this scope.

`GET /api/v1/admin/flags/{name}` reads one configured flag for administrators.
Names are 1–128 characters and match `^[a-z][a-z0-9_.-]{0,127}$`. The read
queries PostgreSQL by the flag primary key, so it reflects updates immediately
without using the 30-second snapshot cache. It returns the standard success
envelope, a `self` GET link, a strong ETag over the complete response bytes, and
`Cache-Control: private, no-store`. The strong validator is the SHA-256 hash of the complete JSON body, including
the final newline, links, and envelope metadata. Successful responses set body
`trace_id` to `""` so identical flags have identical response bytes and ETags
across requests. The actual valid request trace ID is returned in `X-Trace-Id`.
Problem responses retain the actual trace ID in their JSON body. Empty `user_ids` values are represented as `[]`. Query
parameters are not supported.

The `FeatureFlags` service caches flag definitions for up to 30 seconds. A
successful update clears the cache in the current process immediately; other
API processes refresh within the cache window. Administrator membership is
checked against the current user row when evaluating an `admins` audience.

The service returns an error when the database cannot be read. Callers should
propagate that error and avoid enabling behavior based on an incomplete lookup.
