# XFlickr business rules

XFlickr is a multi-user Flickr crawler and searchable photo library. These rules
guide feature implementation; operational environment rules live in
[`ENVIRONMENT.md`](ENVIRONMENT.md).

## Accounts and BYOK

- Each user connects their Flickr account through Flickr authorization and
  supplies their own Flickr API key and secret in the application UI.
- Store API credentials encrypted. They are never shared with other users and
  must not be accepted through the CLI or environment configuration.
- Adding a key checks it with one `flickr.test.echo` call before saving. A
  rejected key returns `key_invalid` and is not stored.
- The public key is stored in plaintext for Flickr calls. API responses show
  the first 6 characters and mask the rest. The secret is sealed with
  associated data `api_keys:<id>`. No endpoint returns the secret, and logs
  do not record it.
- A key stores an hourly limit, a per-second limit, and a burst. Those limits
  stay within the administrator settings: `per_second_rate`, `burst`, and an
  hourly ceiling of 3,600. A positive `daily_call_limit` below 3,600 tightens
  the hourly ceiling to that budget.
- Changing a key secret marks that key's connected accounts
  `reconnect_required`.
- Deleting a key publishes `key.deleted` so queued work for that key stops.
- Key status is `valid`, `invalid`, or `revoked`. `revoked` is terminal.
- Flickr calls consume quota belonging to the API key that made each call. Track
  and enforce quota per key, including its hourly limit and call rate. Reserve
  capacity for user-started actions when the configured quota policy requires
  it.

## Sessions and access

- A successful login sets the `xf_session` cookie. The cookie is HttpOnly,
  Secure, and SameSite=Lax. Its value is an opaque id. The database stores the
  SHA-256 hash of that id, with a sliding expiry of 14 days.
- Each use of a valid session can slide the expiry forward. The stored expiry
  is updated at most once per minute.
- Unsafe methods (POST, PUT, PATCH, and DELETE) require a double-submit CSRF
  token. The `xf_csrf` cookie and the `X-CSRF-Token` header must match. A
  missing or mismatched token is rejected with 403. Safe responses issue the
  CSRF cookie when the browser does not already have one.
- `RequireUser` allows a request only when the session resolves to an active
  user. A missing or expired session responds with 401. `RequireAdmin` applies
  that same check, then responds with 403 when the signed-in user is not an
  administrator.
- Administrators list users (`GET /admin/users`) and change a user's role or
  disabled state (`PATCH /admin/users/{id}`). The last active administrator
  cannot be disabled or demoted, and each change is recorded through the audit
  port.
- `DELETE /auth/sessions` revokes every session belonging to the current user
  except the session that sent the request. Logout revokes the current session
  and clears the session cookie.
- Feature flags default off when unknown. An enabled flag can target all users,
  active administrators, or explicit user IDs. Admin updates take effect in
  the current API process immediately and in other API processes within 30 s.

## Crawl depth

- Depth is configured per XFlickr user, with an administrator-defined default
  and maximum.
- Store each person's interest depth per user. When another path finds the same
  person for that user, keep the lowest depth; ignore discoveries beyond that
  user's effective maximum depth. A person can be expanded when at least one
  interested user has not reached their own maximum depth.
- Connecting a Flickr account registers its owner as a depth 0 interest for
  that XFlickr user.
- Depth 1 follows contacts of that user's connected Flickr accounts. Further
  levels discover owners of photos favorited by contacts at the prior level.
- A user's depth setting bounds that user's discovery. Do not crawl deeper on
  their behalf because another user selected a greater depth.

## Sharing and privacy

- Sharing is an explicit per-user choice. When enabled, that user's eligible
  crawled public records can be reused by all XFlickr users, avoiding duplicate
  Flickr calls.
- When disabled, the user sees records crawled through their own access; records
  are not contributed to the shared pool.
- Record observations belong to a user and connected Flickr account and preserve
  the first time that account saw each record. Sharing is off by default, and
  every change to the user's sharing choice publishes `sharing.changed` through
  the transactional outbox.
- Never share API keys, secrets, OAuth tokens, private photos, or downloads.
  Apply Flickr visibility and authorization rules even when a record is
  otherwise shareable.
- Catalog read endpoints reuse the same visibility policy as other photo reads.
  Omit hidden photos from lists and answer hidden detail reads with 404, the
  same response as a missing row. Exclude tombstones and report only visible
  photo counts for people and albums. Use opaque keyset cursors for list pages.

## Crawl work and quota

- Schedule a per-person change check using each user's `change_check_interval`
  (default 24 hours). Check photos with
  `flickr.people.getPhotos(min_upload_date=last_seen, per_page=1)` and
  favorites with
  `flickr.favorites.getList(min_fave_date=last_fave, per_page=1)`. A positive
  `total` queues the corresponding incremental photo or favorite pages with
  the same minimum date. The scheduler advances a per-user cycle cursor only
  after visiting each person, then wraps to the first person.
- Independently reconcile each person's complete photo and favorite lists on
  the user's `reconcile_interval` (default 30 days). Apply tombstones only after
  every page succeeds, retain tombstoned rows, exclude removed favorites from
  default lists, and publish `photo.visibility_changed` whenever a stored
  photo's visibility changes. A non-owner scan must not tombstone private,
  friends-only, or family-only rows, or remove missing favorites, based on an
  incomplete view.
- A `flickr.photos.getContactsPhotos` response for a connected account may flag
  multiple direct contacts for change checks in one call.
- Schedule Flickr work in two lanes: a person lane for contacts, photos,
  favorites, albums, and related records; and a sizes lane for retrieving photo
  size URLs.
- Confirm photo sizes with `flickr.photos.getSizes` and record the check time.
  Skip a confirmed result while the photo's `last_update` is unchanged. Mark a
  photo unavailable after Flickr reports not found or permission denied, and do
  not retry that photo in the sizes lane.
- Share each key's configured hourly and per-second allowance across its lanes.
  Keep the configured reserve for user-started requests when the key approaches
  its limit.
- Store queued and retryable work in the durable buffer queue so workers can
  claim work, retry transient failures with backoff, and recover after restart.
  Respect priorities and per-key quota when dispatching jobs.
- Work that failed only from lease expiry is retried automatically after 15
  minutes with attempts reset, and `xflickr requeue` returns failed work to
  pending; both go through the normal pump and quota gate.
- Enqueue jobs in the same database transaction as the work they represent.
  Configure queue concurrency and job timeouts, maximum attempts, and backoff by
  queue and job kind. Mark exhausted jobs discarded and publish them to the
  `job.dead` outbox topic.
- Represent one unit of work per backlog row and deduplicate by work kind,
  subject, and user. Keep page progress in a JSON cursor; only advance to the
  next page after the current item finishes. Completed rows reopen only when
  explicitly requested.
- Reuse already-crawled eligible records when sharing permits it; crawl each
  shared record once where possible.
- Expose active crawl backlog by person and sizes lane. ETA is backlog calls
  divided by effective calls per hour from the user's connected valid Flickr
  keys, applying configured hourly, per-second, daily, and lane-share limits.
  Administrators can inspect backlog by plan and lane, oldest active item age,
  and available River jobs by worker queue. Publish aggregate `queue.state`
  snapshots through the outbox once per minute.

## Downloads and cloud sync

- Download originals only as temporary files on the configured BlobStore
  location. Use recorded size URLs so a download does not spend another Flickr
  API call.
- Stream user-scoped `download.progress`, `download.ready`, and
  `crawl.progress` events over authenticated `GET /events`. Event frames carry
  IDs so clients can resume with `Last-Event-ID`; Redis pub/sub distributes
  events to each API instance.
- Apply download back-pressure when the BlobStore is full or its local
  filesystem is more than 90% used. Reject newly requested downloads with
  `temp_storage_full` and stop pumping media work until capacity is available.
- Verify downloaded files before packaging or syncing. ZIP-part links are
  owner-scoped and short-lived; delete the temporary ZIP parts after all
  transfers are acknowledged or the download expires. For cloud sync, delete
  temporary files after upload completes or is abandoned.
- Do not turn temporary download storage into a permanent XFlickr archive. The
  user's connected cloud destination is the durable copy when they choose sync.

## Plans

- Plans determine a user's entitlements and configured limits, including crawl
  depth and access to product capabilities.
- Enforce plan limits at the application boundary for that user, independently
  of shared records and other users' entitlements.
- Keep plan definitions and numeric limits configurable. This specification does
  not establish prices or a fixed plan catalog.
- The initial catalog contains `free` and `pro` plans. Their scheduler weights
  are 1 and 4, and their resource shares are 0.2 and 0.8 respectively. An
  administrator can edit plan limits and assign a plan to a user; both actions
  are recorded in the audit trail.
- Scheduler priority combines plan weight, direct action, starred status,
  remaining depth, and age in minutes. Each coefficient is an application
  setting. Plan resource shares allocate shared scheduler capacity in
  proportion to eligible backlog, and unused allocation is lent to another
  plan with remaining work so free users continue to progress.
- A user can star a contact, select it for seven days, or pause and resume its
  crawl work. Star and selection changes invalidate scores on that user's
  active backlog rows so the scheduler recomputes priority from the updated
  flags.

## Definition of done

A change is done when it meets its ticket's acceptance criteria, preserves these
business and privacy rules, has the required unit and integration coverage,
passes the repository checks, and updates feature documentation and ADRs when
applicable. Changes must not expose secrets or use the live database in tests.
