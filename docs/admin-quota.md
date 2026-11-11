# Admin API quota view

The administrator quota page reads `GET /api/v1/admin/quota` through the
generated web API client. The endpoint includes valid usable keys only, so an empty key list means no
valid key is available; invalid or revoked keys may still exist. It shows each
key's masked label, associated user ID, reporting node, current hourly use and
cap, state, and reset time.

The page shares the quota query with the administrator shell. While visible and
Live is enabled, it polls starting at 10 seconds and backs off to 20, 40, then
at most 60 seconds after consecutive failures. A successful refresh resets the
interval to 10 seconds. Pausing Live stops polling; resuming fetches a fresh
snapshot. Failed refreshes retain the last snapshot, mark it stale, and offer
retry. Initial loading, empty, request failure, forbidden, and stale states do
not expose key material or user email addresses.

The current contract does not return node summaries, lane totals, burst values,
history points, cooldowns, or key pause/move controls. The view identifies those
blocks as unavailable and does not infer values from key state or call routes
that are absent from OpenAPI.

The API call log screen and its bounded, time-filtered keyset endpoint are a
separate remaining slice of JOOXF-182. The UI does not render synthetic call
records.
