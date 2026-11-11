# Home screen

The first Home slice replaces the `/` placeholder with the signed-in user's
contact counts from `GET /api/v1/me/home`. It displays the total, direct, and
discovered contacts and formats each value with the active locale.

The page keeps the user shell visible while the counts load. A failed request
shows a localized error and retry action. A 403 shows an access-denied state;
its sign-in action revokes the current session before navigating to `/login`.
If logout fails, the page stays forbidden and shows a localized retry message.
A 401 is handled by the shared session flow. Server error details are never
displayed.

The current API returns exact contact counts from the user's `crawl_interests`:
depth one contributes to direct contacts, depths greater than one to discovered
contacts, and depth zero is excluded. It has no per-user row limit, so the UI
makes no claim that the count query is bounded. A count of zero means zero
contacts; it does not indicate first-run state. Home does not call Flickr or
infer photo state.

Photo counts, starred contacts, and activity are unavailable in this slice and
are not shown as zero or empty. The page adds no polling, SSE, search, greeting,
or placeholder destinations. The API contract, SQL, schema, generated code, and
settings are unchanged.
