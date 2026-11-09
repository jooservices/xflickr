# Admin API quota view

The administrator quota page reads `GET /api/v1/admin/quota` through the
generated web API client. It shows each returned key's masked label, associated
user ID, reporting node, current hourly use and cap, state, and reset time.

The page shares the quota query with the administrator shell and refreshes it
every 10 seconds while visible. A refresh keeps the last successful snapshot
on screen. Initial loading, empty, request failure, forbidden, and stale states
are presented without exposing key material or user email addresses.

The current contract does not return node summaries, lane totals, burst values,
history points, cooldowns, or key pause/move controls. The view identifies those
blocks as unavailable and does not infer values from key state or call routes
that are absent from OpenAPI.

The API call log screen and its bounded, time-filtered keyset endpoint are a
separate remaining slice of JOOXF-182. The UI does not render synthetic call
records.
