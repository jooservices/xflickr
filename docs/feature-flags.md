# Feature flags

Feature flags let the team merge a feature before enabling its behavior. Every
unknown flag is off. An enabled flag can target everyone, active administrators,
or a configured set of positive user IDs.

`GET /admin/flags` lists configured flags. `PUT /admin/flags` atomically creates
or updates the supplied flags; flags omitted from the request remain unchanged.
Both routes require an administrator session, and the write route also requires
the normal CSRF token. There is no feature flag UI in this scope.

The `FeatureFlags` service caches flag definitions for up to 30 seconds. A
successful update clears the cache in the current process immediately; other
API processes refresh within the cache window. Administrator membership is
checked against the current user row when evaluating an `admins` audience.

The service returns an error when the database cannot be read. Callers should
propagate that error and avoid enabling behavior based on an incomplete lookup.
