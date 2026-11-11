# Admin plans: read-only Defaults slice

ADMIN can inspect server plans at `/admin/defaults`. The existing generated
`GET /admin/plans` client supplies names, scheduler weights, depth/contact caps,
AI quotas per day, ZIP retention hours and resource shares. Numbers use the
selected EN/VI locale; resource shares are percentages. Desktop tables become
labelled cards at 720px and below using the existing quota page styles.

Initial loading has an announced skeleton; empty results have an empty state.
Initial failures offer Retry. Refresh preserves the last successful snapshot
and marks it stale on failure. Error messages are localized, never server text.
A 401 uses the shared session-expiry flow. A 403 cancels the request, removes
the plans query from cache and shows the shared forbidden state. USER access
is rejected by the existing AdminGuard before any plans request.

Refresh is manual: no polling, USER event subscription or Live label is added.
Global defaults, feature flags, plan editing, revisions and durable audited
writes are deferred. No Save/Discard controls or placeholder links are shown.
This delivers only part of JOOXF-186 AC2; neither AC2 nor the ticket is complete.

No REST contract, generated code, SQL, registry setting or migration changes
are included. The existing Plans query has no row limit or `(weight,name)`
index; it can use a sequential scan and sort. This flow is not claimed to be
bounded. A later backend slice must address limits/indexes and honest query
plan tests before that guarantee is possible. No large catalog tables or
Flickr calls are involved.

Component tests use MSW for server values, states, session expiry, cache
removal, localization and read-only access. The mocked browser journey in
`web/e2e/08-admin-defaults.spec.ts` checks refresh, mobile layout, accessibility,
console cleanliness and USER denial; it does not require a backend database.
