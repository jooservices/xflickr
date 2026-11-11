# Crawl call audit (JOOXF-310)

Audit date: 2026-11-06. Machine: `dev-workspaces` (192.168.10.100).
Scope: every registered person facet, change checks, reconciliation, and the
photo lane. Sources: JOOXF-310 -> JOOXF-281 -> JOOXF-8; the feature documents
linked below; current code; worktree PostgreSQL fixtures; official Flickr
references. No live database query, live Flickr request, deployment, or cleanup
of existing queued work was performed.

## Evidence and limits

**Lead-provided historical facts:** hourly calls: favorites 540, people photos
216, people groups 198, albums 15, galleries 9, sizes 0. The five person methods
sum to 978 calls/hour. Catalog: approximately 3.16M photos, 1.59M favorites,
131k albums, 5k galleries, zero groups and no contact relationships. Pending
queue: approximately 125k items, oldest approximately 16 hours. These are not
measurements taken by this audit. Catalog totals are cumulative, not rows per
hour, so dividing them by hourly calls would be misleading.

**Verified defect:** `facets/catalog.go` decoded all four counts (`photos`,
`count_photos`, `members`, `pool_count`) as plain `int64`. Flickr's existing
client fixture `backend/pkg/flickr/testdata/people.getGroups.json` contains
`"members":"150"`. A successful HTTP response can therefore fail parsing,
produce no catalog write, and repeat the same API page up to the engine's five
default attempts. Numeric-only facet fixtures had hidden this defect. Groups
already have a complete persistence path: `Groups.Handle` -> `writeCatalog` ->
`UpsertGroups` and `UpsertPersonGroups` -> transactional catalog repository.
The production `observedCatalog` wrapper embeds those methods; it does not
silently discard groups.

**Inference:** the reproduced parsing failure can explain successful
`people.getGroups` calls with no stored groups. It does not prove the live
responses contained groups or string counts. Empty successful replies, a
runtime built from another revision, and other persistence failures cannot be
excluded from aggregate method counts alone. Do not disable the facet merely
because its live catalog is empty.

## Per-facet call and storage inventory

Let `P(n) = max(1, ceil(n / 500))`, including the request needed to learn a list
is empty. Rows below mean records submitted to idempotent upsert, not guaranteed
new rows. Retries, failures and crashes can add calls. Conditional savings are
estimates, not additive with the follow-up proposals.

| Facet / method | Calls per entity or cycle | Rows stored per successful call | Waste / finding | Estimated hourly saving from this PR | Fix / disposition |
| --- | --- | --- | --- | --- | --- |
| `photos` / `people.getPhotos` | Initial `P(photos)`; incremental `P(new photos)` after positive probe | Up to 500 photos plus distinct owners and observer records | Initial discovery now stores a scan-start checkpoint (JOOXF-345); independent reconcile/check can still reread the same list | 0 attributed from supplied 216/h | Follow-up 2 delivered; follow-up 3 remains; retain URL extras and privacy |
| `favorites` / `favorites.getList` | Initial `P(favorites)`; incremental `P(new favorites)` | Up to 500 photos, timestamped favorite relationships, distinct owners and observations | Relationship omitted if neither `date_faved` nor `date_favorite` is positive; photo still stored. Depth limit blocks owner discovery, not this list | 0 attributed from supplied 540/h | Follow-ups 2 and 3; no invented favorite timestamps |
| `albums` / `photosets.getList` | `P(albums)` initially and per catalog refresh | Up to 500 album metadata rows; no album-photo relationship fetch | String `photos` causes page parsing failure and retries | 0–12/h of supplied 15/h under five-attempt model | Accept existing Flickr numeric-string type |
| `galleries` / `galleries.getList` | Continuation cursor initially and per refresh; first request uses `continuation=0`, subsequent requests relay the response cursor until `-1` | Up to 500 gallery metadata rows; no gallery-photo relationship fetch | String `count_photos` is accepted; cursor progress commits with gallery rows. Missing continuation falls back to the legacy page counter; repeated cursors and empty nonterminal pages stop with a warning | No measured quota change; prevents repeated or skipped continuation pages | Delivered in JOOXF-349 |
| `groups` / `people.getGroups` | Normally 1 unpaged response initially and per refresh; current handler follows `pages` if supplied | One group and one membership per returned group | String counts previously failed the whole page (fixed). Large unpaged lists are committed in ≤500-row chunks in one transaction (JOOXF-352). Official method documents no paging arguments | 0–158.4/h of supplied 198/h under five-attempt model | Delivered: numeric counts; proposal 9 |
| `check` / photo then favorite list | 2 probes/person; each uses `per_page=1`; positive probes add incremental work | 0 catalog rows from probes; durable work/check state only | Probe sample deliberately discarded. Always refreshes albums, galleries, groups after second probe, including zero/skipped favorite probe | No measured saving | Follow-ups 2–4; do not replace probes with a full scan |
| `reconcile` / photo then favorite list | `P(photos) + P(favorites)` per person | Same photo/favorite rows; final transaction applies visibility-safe diff | Separate kind from initial/incremental work, so overlapping full scans are possible; no cheap probe can detect deletions | No measured saving | Follow-up 3; keep complete-scan and owner-only diff protections |
| `contacts_photos` / `photos.getContactsPhotos` | 1/account at due cycle wrap; `count=50` is maximal | 0 photos or contact relationships; distinct known owners become check work | Response photos intentionally used only as hints. Reopening a completed check does not consult `last_checked_at` | No measured saving | Follow-up 4; unknown interests already rejected by PostgreSQL enqueue |
| Contacts / `contacts.getList` | 0: not a registered production facet | 0 contact relationships | Missing integration, not a wasteful call; recent-contact photos cannot enumerate contacts with no photos | 0 | Follow-up 5; adding a Flickr method is outside parent scope |
| `sizes` / `photos.getSizes` | 0 while unchanged/placeholder/ineligible; 1 per eligible version | One sizes result and check timestamp | Demand and terminal/version guards already implemented; supplied baseline is 0/h | 0 | Preserve; queued cleanup belongs to JOOXF-311 |
| Optional `info`, `exif`, `contexts` | 1 each per eligible photo version | One stored facet payload and status each | Scheduler filters completed versions; worker checks size readiness/restrictions but not already-completed facet version | Unknown; no supplied rate | Follow-up 7; preserve admin toggles and caller restrictions |
| Optional `comments`, `photo_favorites` | List pages, bounded at 200 requests/job | One combined facet payload/status only after all pages finish | Failure late in the job refetches earlier pages. Roles request 50; photo favoriters maximum is 50, not 500 | Unknown; both disabled by default | Follow-up 7; investigate actual method pagination before changing it |

## Repetition, checkpoints and intervals

**Verified:** `ChangeCheck` requests `min_upload_date=last_seen_at` then
`min_fave_date=last_fave_at`; positive totals create 500-item incremental work
with the same lower bound and cycle start checkpoint. `finishCrawlSQL` advances
the corresponding interest checkpoint only after `photos` or `favorites`
finishes successfully with a positive `checkpoint_at`. An unchanged empty
probe does not advance that checkpoint. Flickr documents the photo lower bound
as inclusive: retaining the same boundary is safe for late arrivals but can
reread boundary records. Do not blindly add one second and lose records.

Initial `Registry.Plan` photo and favorite work receives a checkpoint when it
is enqueued; a missing value is filled before its first page request. The
successful final page records a separate completion marker and updates the
interest checkpoint in the same transaction. The first cycle can queue both
`check` and `reconcile` because null timestamps are due. Reconciliation
completion updates only `last_reconciled_at`, not the incremental checkpoints.
Consequently initial pages, a positive check from zero, and reconciliation may
read unchanged data under different work kinds. This is not proof that all
540 favorite calls/hour are avoidable: their result sizes and work kinds are
not present in the supplied baseline.

**Verified:** the normal cycle gates checks at the effective user interval
(default 24h), reconciles independently at the effective user interval
(default 30d), and stamps `last_checked_at` at enqueue time. Reconciliation is
not triggered every 24h after a successful full scan. Album/gallery/group
refresh is tied to the completed check, not to 30d reconciliation. Their lists
are refreshed even for a person with unchanged or empty photos/favorites;
there is no count/version probe for those catalogs in this implementation.
A contacts-photos hint can reopen a completed check sooner than 24h. Its
checkpoint parameters are safely replaced from the known user's stored
interest; it does not reset a known person to timestamp zero.

## Depth, duplicate paths and facet order

**Verified:** discovery writes lowest depth per `(user, person)` and rejects
`depth + 1 > effective MaxDepth`. A discovery at exactly maximum depth still
plans albums, favorites, galleries, groups and photos, even when it later
returns no photos, albums or favorites. It can still contribute its visible
favorites and group/gallery metadata; an empty photo list does not prove that
other facets are empty or that the person will remain inactive. This PR does
not introduce a depth-2 shortcut or change discovery eligibility.

`Registry.Plan` iterates names alphabetically: albums, favorites, galleries,
groups, photos are the facets with initial plans. Checks and reconciliation
have no initial plan. Work creation order is not a dispatch guarantee: the
scheduler orders by score, due time, then ID. There is no enforced cheap-probe
barrier before full initial scans. Checks themselves probe photos before
favorites; reconciliation completes photos before favorites.

Several paths can request the same person facet: repeated favorite-owner
discovery, cycle checks, contacts hints, and catalog refresh. PostgreSQL dedupes
`(kind, subject_key, user_id)`; an active row keeps its cursor even when a
producer asks to reopen it. Completed initial plans do not reopen. Explicit
check refreshes reopen terminal rows. Different users retain distinct work,
and `reconcile` and `photos`/`favorites` are different kinds, so that uniqueness
does not eliminate cross-kind or cross-user rereads. Any future shared-call
optimization must preserve observation ownership and access/privacy.

## Scheduler value and starvation

**Verified:** score = plan weight × 10 + direct action × 100 + starred × 20 +
remaining depth × 5 + age minutes × 0.1 at registered defaults. Admin settings
control each coefficient; those are not observed live values. Rows are chosen
by descending cached score, then `not_before`, then ID, within per-plan share
allocation; unused plan allocation is lent. Every two-second pump refreshes up
to 500 due pending scores, rotating by oldest score update. At 125k eligible
rows a complete ideal sweep takes about 250 ticks / 500 seconds (8m20s), not
instant reprioritization; actual locks and eligibility can increase latency.

**Inference:** there is no facet-value/cost factor. A large old favorites scan
can outrank a new cheap probe; creation age survives page advances and
reopening, so the original age of a long-lived row can favor repeated work.
At defaults 16h adds 96 points, a direct action equals 16h40m of aging, and a
star equals 3h20m. These affect waiting time; the aggregate oldest age does not
prove permanent starvation of a specific facet or user. Finite backlog with
positive aging and plan shares is different from sustained inflow, cached-score
lag, or an admin setting age weight to zero. Preserve fairness, depth and
interactive reserve rather than changing weights based on totals alone.

## Small fix and one-hour fixture evidence

The only production change uses existing `flickr.Int` at the catalog transport
boundary, converting to `int64` when constructing domain records. No settings,
methods, endpoints, schema, depth, permissions, quota or scheduling rules
change. Both numeric and string counts now persist; malformed JSON still
fails. Unit regressions were run and failed before this fix for all three
facets.

`TestCatalogCallAuditHour` uses the real engine, PostgreSQL transactions and
retry policy, a fake HTTP boundary and a virtual 60-minute clock. Twelve people
arrive four minutes apart (0 through 44m), each requesting one album, gallery
and group. Dispatch honors stored retry deadlines; terminal work cannot call
again. All five default attempts fit in the hour. The governor is a test
permit, so this is a one-hour workload replay, not a production quota-cap or
scheduler benchmark.

| Same one-hour fixture | Before fix | After fix |
| --- | ---: | ---: |
| Album calls | 60 | 12 |
| Gallery calls | 60 | 12 |
| Group calls | 60 | 12 |
| Total calls | 180 | 36 |
| Completed work items | 0/36 | 36/36 |
| Stored albums / galleries / groups / memberships | 0 / 0 / 0 / 0 | 12 / 12 / 12 / 12 |

**Conditional live estimate:** for an affected workload that currently spends
five calls per failed page, successful decoding removes four of five calls
(80%). Applying that assumption to the supplied catalog method counts yields
at most 177.6 calls/hour saved (158.4 groups + 12 albums + 7.2 galleries), or
18.2% of the five supplied person-method counts. The defensible lower bound
is zero until the affected-response fraction is measured. Do not promise a
198/h reduction or remove every group call: useful first calls remain.

**AC2 limit:** no post-deployment full live quota hour is available in this PR.
The lead must deploy the reviewed revision, observe one complete key quota
window, and compare method/facet calls, parse failures, completed items and
newly stored groups/memberships against a comparable window. Count granted
calls separately from `quota_wait`, include HTTP-success/parse-failure cases,
and use partition-pruned recent-window bounded aggregates, never a full
call-log scan. Record window bounds, revision, key budget and demand; reduced
calls alone can also mean stalled work. This external live verification remains
open; the fixture numbers must not be reported as satisfying it.

## Proposed follow-up tickets (not created)

1. **Measure catalog decode failures and live call yield.** Attribute HTTP
   successes that fail parsing separately; sample safe response structure
   without secrets; compare complete quota windows and rows/relationships
   persisted. First establish whether the 198/h group baseline is affected.
2. **Establish incremental checkpoints after initial scans.** **Delivered in
   JOOXF-345.** Carry a stable scan-start checkpoint through page commits and
   update only after a successful final page. The offline `repair-checkpoints`
   command uses observer-scoped completion evidence; legacy work without
   markers gets a conservative full-list catch-up floor. Cover inclusive
   timestamps, late arrivals, failed pages and caller scope.
3. **Coordinate initial, check and reconcile work.** Avoid simultaneous full
   lists for the same observer while retaining discovery from favorites and
   complete owner-aware deletion diffs. Reconciliation intentionally suppresses
   new owner discoveries, so dropping the ordinary favorites scan wholesale
   is incorrect. Measure overlapping work before estimating savings.
4. **Bound catalog refresh and contacts-hint repetition.** Define admin-UI
   configurable catalog intervals and hint coalescing, with freshness policy
   agreed by the lead. Test unchanged/empty people, max-depth interests,
   concurrent producers and paused contacts. No permanent empty-person skip.
5. **Restore explicit direct-contact enumeration.** The crawl has no production
   contacts facet or writer call despite the catalog contacts repository.
   Agree a separately authorized scope because the parent excludes new Flickr
   methods; preserve the depth-1 contact / later favorite-owner rules.
6. **Implement and test gallery continuation.** Delivered in JOOXF-349.
   Flickr documents request parameter `continuation`, response field
   `continuation`, initial value `0`, and terminal value `-1`. The handler
   forwards each opaque response cursor and commits it with the gallery rows.
   Legacy responses still use page/pages; repeated cursors and empty pages with
   nonterminal cursors stop with a warning before another Flickr call.
7. **Harden optional-photo facet execution.** Skip already-completed versions
   at execution, persist pagination progress across quota waits/retries, and
   replace operational page bounds with admin-UI settings if required. Verify
   method-specific limits: photo favoriters max 50, not 500. No supplied hourly
   rate supports a numerical saving yet.
8. **Measure scheduler fairness by facet and work age.** Compare arrival rate,
   completion rate, cost and wait by facet/user/plan. Test sustained old paged
   favorites against new probes and selected/starred work, plus rescore lag at
   125k rows and reopened-row age. Any value priority or probe dependency must
   retain aging, plan lending and quota reserves; do not tune live weights here.

9. **Commit large unpaged group lists in bounded batches.** Delivered in
   JOOXF-352. `people.getGroups` is unpaged and can return more than 500
   memberships. Group and person-group upserts now share a reusable
   `forEachChunk` helper that writes at most 500 rows per repository call
   inside the existing crawl commit transaction, so a Result with 501–5,000
   memberships commits completely without a second Flickr fetch for a
   batch-size failure. The single-chunk 500-row guard remains for writers
   that do not go through the helper.

## References

- [Crawl engine](crawl-engine.md), [interests](crawl-interests.md),
  [reconciliation](reconciliation.md), [entity state](entity-state.md),
  [photo lane](photo-lane.md), [quota](quota.md), [schedule](schedule.md).
- Code: `backend/internal/crawl/facets/{catalog,photos,favorites,reconcile}.go`,
  `backend/internal/crawl/app/{check,engine,registry,cycle,photo_facets}.go`,
  `backend/internal/crawl/adapters/postgres/{cycles,engine}.go`,
  `backend/internal/platform/roles/{crawl_engine,crawl_observations}.go`,
  `backend/internal/schedule/adapters/river/enqueue.go`,
  `backend/internal/settings/domain/settings.go`.
- Official Flickr: [people groups](https://www.flickr.com/services/api/flickr.people.getGroups.html),
  [people photos](https://www.flickr.com/services/api/flickr.people.getPhotos.html),
  [person favorites](https://www.flickr.com/services/api/flickr.favorites.getList.html),
  [albums](https://www.flickr.com/services/api/flickr.photosets.getList.html),
  [galleries](https://www.flickr.com/services/api/flickr.galleries.getList.html),
  [contacts photos](https://www.flickr.com/services/api/flickr.photos.getContactsPhotos.html),
  [photo favoriters](https://www.flickr.com/services/api/flickr.photos.getFavorites.html).
