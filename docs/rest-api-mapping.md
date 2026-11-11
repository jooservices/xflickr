# REST API route mapping (JOOXF-314)

Audit date: 2026-11-06. Machine: `dev-workspaces` (192.168.10.100).
Baseline: `f572507` on `develop`. This is an implementation-source audit, not
an exercise against the live database or real Flickr. No runtime behavior,
contract, business rule or binding REST rule is changed by this document.

[REST rules](rest-api.md) and the referenced Workspaces HTTP API standard
(`/home/joos/Workspaces/.claude/skills/http-api-design/references/standard.md`)
are binding. This completes the starting mapping; actual current methods below
come from the contract and router, rather than the illustrative starting table.
Recommendations marked D1–D8 need lead decisions before their affected redesign
sub-task ships. Other target names are design proposals applying the rules.

## Inventory and counting

**55 existing API operations: 0 fully conforming, 55 need changes.** Of these,
53 require a path change (including the callback split and internal heartbeat);
2 probes keep their paths but their JSON bodies do not implement rules 4/5/9.
Count a route once per explicit method and path, even when its implementation
is also mounted on another listener. The tables below contain all 55 operations,
not just the verb-containing URLs. Browser routes are inventoried separately
because HTML pages are explicitly outside the API rules; they are not counted
as nonconforming API operations. Proposed new supporting routes are also
separate and are not included in the 55.

Verified source inventory:

- `contracts/openapi/openapi.yaml`: resolve every path reference into all 17
  files in `contracts/openapi/paths/`; include shared path-item parameters.
- `backend/internal/platform/roles/routes.go`, generated registration in
  `backend/internal/platform/api/gen/server.gen.go`, and
  `roles/builtin.go:newAPIRoutersWithServices`: 55 access entries/operations.
  SSE, probes and heartbeat already occur in OpenAPI here; do not count them
  again as undocumented routes.
- `backend/internal/platform/httpx/{health,readiness}.go`: non-API roles also
  register `GET /healthz` and `GET /readyz`; same logical operations, same target.
- `backend/internal/cluster/adapters/http/handler.go`: public internal-path
  blocker and internal heartbeat-only guard expose just the heartbeat on the
  internal listener; generated registration does not make its other routes
  reachable there. No extra product operation was found in adapters.
- `backend/internal/platform/roles/web_assets.go`, `web/src/app/router.tsx`
  and `web/src/app/shells/nav.ts`: HTML/HEAD pages, assets and SPA fallback.
- `backend/internal/platform/sse/handler.go`: alternate direct SSE handler has
  a `lastEventId` query fallback; the generated route only declares the header.
- `backend/internal/fakeflickr/server.go` and `accounts/adapters/flickr/fake*.go`
  implement fake *external provider* REST/OAuth/image/scenario routes for tests,
  not XFlickr product routes. Keep those provider contracts unchanged.
- `backend/cmd/xflickr`: migrate, crypto, requeue and observations are local
  operator commands, not an HTTP CLI client. No shipped product HTTP CLI executable was
  found. `docker/scripts/develop-smoke.sh` does call `/admin/ops/status`
  and `/healthz`, and `docker/scripts/e2e.sh` polls `/healthz`; migrate the
  smoke script with ops. CLI impact below also covers external scripts using
  old documented routes, without claiming an existing product CLI uses them.

## Reading every row

Current statuses are the explicitly documented contract statuses (including
`default`); middleware can also produce binding/auth/CSRF/500 errors. The target
column specifies the success change; **P applies to every API error in every
row**, including errors on 204 and SSE routes. Existing `{code,message}`,
registration field/conflict shapes and empty error bodies become P.

- **E**: JSON `application/json` envelope with exactly `success`, `code` equal
  to HTTP status, `message`, `data`, `meta`, `errors:null`, `trace_id`. Public
  DTOs only, timestamps UTC RFC 3339, explicit duration/size units; secrets
  omitted and Flickr keys masked. Each resource, including array members and
  embedded resource objects, carries `links`.
- **P**: `application/problem+json` with `type:/problems/<hyphenated-code>`,
  `title`, `status`, safe `detail`, `instance`, `trace_id`, and field-to-message
  arrays in `errors`. Keep domain distinctions (`key-invalid`,
  `depth-above-cap`, `temp-storage-full`, `last-admin`, `reconnect-required`).
  Preserve each row's existing meaningful 401/403/404/409/422/503/500 outcomes;
  split current generic 400 into malformed 400 and invalid-value 422.
  Add unknown query/field 422, authentication 401 + `WWW-Authenticate`,
  caller-rate-limit 429 + `Retry-After`, and safe 500 where applicable.
  Another user's resource is 404, not 403. Admin role denial and cookie CSRF
  failure remain 403. Concurrent mutable settings/plans/flags/defaults and
  admin edits require `ETag`/`If-Match` with stale 412 (including empty-success
  writes). Missing precondition handling must be specified in the contract.
- **L**: `Location` of the created resource; for 202 it identifies the status
  resource, whose representation has `status`. **I**: accept `Idempotency-Key`,
  replay the original result for the same key within 24 hours, scope to caller
  and operation, and reject a conflicting payload with 409. Do not repeat work
  or spend another Flickr call on replay. Other retry-sensitive POSTs, such
  as authorizations, also need documented duplicate protection.
- **B**: 204 without body or success envelope. DELETE has no request body and
  remains idempotent. PATCH always uses `application/merge-patch+json`;
  PUT replaces the complete writable representation (not server-owned fields).
- **C**: array `data`, opaque `cursor`, `limit` default 50/max 100,
  `meta.pagination={limit,has_more,next_cursor}`, usable `meta.links.self`
  and `next` when more exists, stable deterministic order with a unique
  tiebreaker. No fabricated/expensive total. Each member has its own links.
  Collection creation links belong in metadata, not a fabricated array member.
- **N**: no pagination/filtering because this is a singleton, detail or write.
  All query parameters are rejected unless explicitly allowlisted in its
  contract. Do not invent search/filter features in this audit. `sort`, if
  exposed, is an allowlist of public fields (for example `-created_at`), with
  an immutable ID tiebreaker; unsupported sorts return 422. Keep existing
  catalog ordering until an explicit sort is specified.

Every listed resource link includes `self` (`GET` of the resource URI) plus the
relations shown, each `{href,method}`. Emit action links only when the caller
is authorized and the current state allows them; absent means unavailable.
No secrets in links. L must resolve to a permitted read, not merely a POST URL.
204s have no links in a body; discover their actions from the owning resource.
SSE frames and raw ZIP transfers are not JSON success envelopes.

Browser auth remains `xf_session` + double-submit CSRF on unsafe methods;
bearer tokens with scopes are a separate mechanism (JOOXF-318), never combined
with cookies. Node tokens remain isolated to the internal listener. GETs must
not perform OAuth exchanges, change crawl state, or fetch missing Flickr sizes.
Use cheap ETags for reads; use private/no-store caching for sensitive identity,
authorization and credential responses.

## Consumer evidence key

Paths below are relative to the repository. A screen marked **planned** is a
placeholder/spec, not proof of a working API call. All API rows require
regenerated Go/TypeScript contracts in JOOXF-316; regeneration alone does not
migrate handwritten response decoding, Zod validators, query keys, mocks or
status branches in `web/src/shared/api/{mutator,validate}.ts`.

| Key | Screens and concrete callers/tests |
| --- | --- |
| I | Login/Register: `web/src/pages/{login,register}/*Page.tsx` and their tests; `features/auth/api.ts`; `web/e2e/01-auth.spec.ts`; logout also `app/shells/{UserShell,overlays.test}.tsx`. Backend `identity/adapters/http/{handler,middleware}_test.go`. |
| P | Profile and session bootstrap: `pages/profile/ProfilePage.tsx`/test, `entities/session/SessionProvider.tsx`/tests; `web/e2e/05-profile.spec.ts`; `identity/app/profile_test.go` and session tests. |
| S | Sharing screen planned (`docs/ui/views/user-sharing.md`); generated sharing client; `sharing/adapters/http/handler_test.go`. |
| K | Keys and Setup: `pages/keys/*`, `pages/setup/SetupPage.tsx`/tests, `features/keys/api.ts`/test, shell banners; `web/e2e/02-keys.spec.ts`; `accounts/adapters/http/handler_test.go`. |
| A | Keys/Setup account connect/list and AccountsSection; shell banners; `web/e2e/03-flickr-account.spec.ts`; `accounts/adapters/http/connect_test.go`, `platform/roles/oauth_flow_test.go`. |
| C | Crawl and Setup: `pages/crawl/CrawlPage.tsx`/test, `features/crawl/api.ts`, `features/keys/api.ts`; contact controls planned in contact detail; `web/e2e/04-crawl.spec.ts`; `crawl/adapters/http/handler_test.go`. |
| U | Admin Users planned (`docs/ui/views/admin-users.md`); generated admin client; `identity/adapters/http/admin_users_test.go`. |
| T | Admin Defaults/User plan screens planned; generated admin client; `schedule/adapters/http/handler_test.go`. |
| F | Admin Defaults flags planned; generated admin client; `settings/adapters/http/flags_test.go`. |
| Q | KeyCard usage via `pages/keys/api.ts`; admin shell `app/shells/data.ts`/shell tests; Admin Quota planned; `quota/adapters/http/handler_test.go`. |
| O | Admin Overview/Metrics/Pipeline planned; generated admin client; `platform/ops/adapters/http/handler_test.go`, `schedule/adapters/http/handler_test.go`, `platform/roles/quota_state_test.go`; `docker/scripts/develop-smoke.sh` calls the ops URL directly. |
| N | Admin Nodes planned; `cluster/adapters/http/handler_test.go`, `platform/roles/routes_integration_test.go`; `platform/roles/builtin.go` registers a node-agent role but no heartbeat HTTP client. |
| D | Downloads/Tray planned; generated downloads client; `media/adapters/http/downloads_test.go`, `platform/roles/routes_integration_test.go`. |
| E | Crawl/download progress planned SSE consumer; `platform/roles/events_test.go`, `platform/sse/handler_test.go`; no EventSource caller found in current web source. |
| B | Library/Contacts/Albums/Search planned placeholders; generated catalog client; `catalog/adapters/http/handler_test.go`, `web/e2e/{04-observations,06-deleted-entity,06-photo-sizes}.spec.ts` use HTTP directly. |
| H | `app/shells/Maintenance.tsx` reads health; `platform/roles/health_probe_test.go`, `platform/httpx/{health,readiness}_test.go`; readiness also deployment checks. |

## Complete existing API mapping

**Breaking column:** W = web/generated-client contract break (even if a screen
is planned), C = old HTTP CLI/scripts break; N = internal node-agent break.
Probe changes are body breaks for JSON consumers, not status-only probes.
All target names include their literal prefix; no implicit `/api/v1` is hidden.

### Identity and sharing

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key | Delivery |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `GET /auth/config` (`GetAuthConfig`) | `GET /api/v1/authentication-options` | 200 → 200 E; public options object | N | users POST, sessions POST | W; C | I | Delivered (JOOXF-353) |
| `POST /auth/register` (`Register`) | `POST /api/v1/users` | 201,400,409 → 201 E + L; user representation | N | me GET, sessions POST | W; C | I | Delivered (JOOXF-353) |
| `POST /auth/login` (`Login`) | `POST /api/v1/sessions` | 200,401,429,423,403 → 201 E + L; session with user, cookie retained; lockout is 429 per D5, not 423 | N | user GET, revocation DELETE | W; C | I | Delivered (JOOXF-353) |
| `POST /auth/logout` (`Logout`) | `DELETE /api/v1/sessions/current` | 204 → 204 B; clear cookie | N | No body; discover from session | W; C | I | Delivered (JOOXF-353) |
| `DELETE /auth/sessions` (`RevokeOtherSessions`) | `DELETE /api/v1/me/sessions?scope=others` | 204,401,403 → 204 B | Allowlist scope=others; no body | No body; discover from me | W; C | P | Delivered (JOOXF-353) |
| `GET /me` (`GetMe`) | `GET /api/v1/me` | 200,401 → 200 E; user | N | sessions GET, password PUT, sharing GET/PUT; crawl GET is withheld until the Crawl slice | W; C | P | Delivered (JOOXF-353) |
| `PATCH /me` (`UpdateMe`) | `PATCH /api/v1/me` | 200,400,401,403,415,422 → 200 E; locale-only merge patch | N | sessions GET, sharing GET/PUT | W; C | P | Delivered (JOOXF-353) |
| `GET /me/sessions` (`ListMySessions`) | `GET /api/v1/me/sessions` | 200,401,422 → 200 E; SessionList → array | Opaque cursor; limit 50 by default, maximum 100; no field filters | revocation DELETE per member | W; C | P | Delivered (JOOXF-353) |
| `DELETE /me/sessions/{id}` (`RevokeMySession`) | `DELETE /api/v1/me/sessions/{id}` | 204,401,403,404 → 204 B; retain missing/hidden 404 | N | No body; discover from session member | W; C | P | Delivered (JOOXF-353) |
| `POST /me/password` (`ChangeMyPassword`) | `PUT /api/v1/me/password` | 204,400,401,403,429 → 204 B; full new credential with current-password proof, not partial user update | N | No body; discover from me | W; C | P | Delivered (JOOXF-353) |
| `PUT /me/sharing` (`PutMeSharing`) | `PUT /api/v1/me/sharing` | 200,400,401,403,422 → 200 E; full sharing choice | N | me GET | W; C | S | Delivered (JOOXF-353) |

JOOXF-353 also delivered the supporting identity reads required to dereference
registration and session links: `GET /api/v1/users/{id}`,
`GET /api/v1/sessions/current`, `GET /api/v1/sessions/{id}`,
`GET /api/v1/me/sessions/{id}`, and `GET /api/v1/me/sharing`. Every legacy
identity route above is removed in the same release; it has no compatibility
alias.

### Accounts, keys and quota

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key |
| --- | --- | --- | --- | --- | --- | --- |
| `GET /keys` (`ListKeys`) | `GET /api/v1/keys` | 200,401 → 200 E; bare array → paged array | C; no field filters | quota GET, update PATCH, removal DELETE; collection creation POST | W; C | K |
| `POST /keys` (`CreateKey`) | `POST /api/v1/keys` | 201,400,401,409,422,503 → 201 E + L; masked key only; retain single echo validation | N | quota GET, update PATCH, removal DELETE | W; C | K |
| `GET /keys/{id}` (`GetKey`) | `GET /api/v1/keys/{id}` | 200,401,404 → 200 E; masked key | N | quota GET, update PATCH, removal DELETE | W; C | K |
| `PUT /keys/{id}` (`UpdateKey`) | `PATCH /api/v1/keys/{id}` | 200,400,401,404,409,422 → 200 E; optional fields become merge patch; no secret readback | N | quota GET, removal DELETE, accounts GET | W; C | K |
| `DELETE /keys/{id}` (`DeleteKey`) | `DELETE /api/v1/keys/{id}` | 204,401,404 → 204 B; key.deleted behavior retained | N | No body; discover from key | W; C | K |
| `GET /keys/{id}/quota` (`GetKeyQuota`) | `GET /api/v1/keys/{id}/quota` | 200,401,404 → 200 E; quota object; units explicit | N | key GET | W; C | Q |
| `POST /flickr-accounts/connect` (`ConnectFlickrAccount`) | `POST /api/v1/flickr-authorizations` | 200,400,401,404,409,422,503 → 200 → 201 E + L; pending authorization with authorize URL | N | completion PUT, accounts GET; authorize GET is external provider URL | W; C | A |
| `GET /flickr-accounts` (`ListFlickrAccounts`) | `GET /api/v1/flickr-accounts` | 200,401 → 200 E; bare array → paged array | C; no field filters | key GET, authorizations POST | W; C | A |
| `GET /flickr-accounts/callback` (`FlickrAccountCallback`) | `GET /flickr-accounts/callback (web page); PUT /api/v1/flickr-authorizations/{state}` | 200,400,401,409,422,503 → Current 200 JSON / browser 302 → 200 HTML safe page, then 200 E authorization completion; never exchange on GET | state, oauth_token, oauth_verifier move from API query into PUT body/state path; web accepts provider query | account GET, key GET; authorization self GET (new) | W; C | A |
| `GET /admin/quota` (`GetAdminQuota`) | `GET /api/v1/admin/key-quotas` | 200,401,403 → 200 E; keys wrapper → array; snapshot time in meta | C; no field filters; preserve per-key node/trend fields | key GET only if authorized, operations GET | W; C | Q |

### Crawl and scheduling

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key |
| --- | --- | --- | --- | --- | --- | --- |
| `GET /me/crawl/queue` (`GetMeCrawlQueue`) | `GET /api/v1/me/crawl/queue` | 200,401 → 200 E; bounded lane summary object | N; fixed person/sizes lanes, not backlog rows | crawl GET, events GET | W; C | C |
| `GET /me/crawl` (`GetMeCrawl`) | `GET /api/v1/me/crawl` | 200,401 → 200 E; progress object | N; bounded configured depth/facets/lanes | settings GET, queue GET, update PATCH (D1), events GET | W; C | C |
| `GET /me/crawl/settings` (`GetMeCrawlSettings`) | `GET /api/v1/me/crawl/settings` | 200,401 → 200 E; depth and read-only caps; ETag | N | update PATCH, crawl GET | W; C | C |
| `PUT /me/crawl/settings` (`UpdateMeCrawlSettings`) | `PATCH /api/v1/me/crawl/settings` | 200,400,401,422 → 200 E; merge patch depth; caps stay read-only; ETag/If-Match | N | crawl GET, queue GET | W; C | C |
| `PUT /me/contacts/{nsid}/star` (`StarMyContact`) | `PATCH /api/v1/me/contacts/{nsid}` | 204,400,401,404 → 204 → 200 E; merge patch starred | N | person GET, crawl GET | W; C | C |
| `PUT /me/contacts/{nsid}/select` (`SelectMyContact`) | `PATCH /api/v1/me/contacts/{nsid}` | 204,400,401,404 → 204 → 200 E; merge patch selected_until; seven-day rule D2 | N | person GET, crawl GET | W; C | C |
| `PUT /me/crawl/pause` (`PauseCrawlContact`) | `PATCH /api/v1/me/crawl (binding starting target; D1)` | 204,400,401,404 → 204 → 200 E; paused=true; current contact nsid semantics unresolved D1 | N | queue GET, settings GET, events GET | W; C | C |
| `PUT /me/crawl/resume` (`ResumeCrawlContact`) | `PATCH /api/v1/me/crawl (binding starting target; D1)` | 204,400,401,404 → 204 → 200 E; paused=false; current contact nsid semantics unresolved D1 | N | queue GET, settings GET, events GET | W; C | C |
| `GET /admin/queue` (`GetAdminQueue`) | `GET /api/v1/admin/queue-snapshots/current` | 200,401,403 → 200 E; bounded plan/lane aggregate, ready worker-queue counts | N; fixed configured dimensions; actual backlog collection would need C | operations GET, quotas GET, events GET | W; C | O |

### Administration, operations and nodes

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key |
| --- | --- | --- | --- | --- | --- | --- |
| `GET /admin/users` (`ListAdminUsers`) | `GET /api/v1/admin/users` | 200,401,403 → 200 E; AdminUserPage → array; exact cheap totals only | page/page_size → C; q → email (same case-insensitive substring); stable created_at,id descending | update PATCH, plan PUT per member | W; C | U |
| `POST /admin/nodes` (`CreateAdminNode`) | `POST /api/v1/admin/nodes` | 201,400,401,403,409 → 201 E + L; one-time token conflict D7 | N | heartbeat PUT (internal origin), node GET (new) | W; C | N |
| `POST /internal/nodes/heartbeat` (`HeartbeatNode`) | `PUT /internal/v1/nodes/{id}/heartbeat` | 200,400,401 → 200 E; full heartbeat report, identity bound to bearer node; no merge semantics | N | node GET (authorized internal representation needed) | N; C | N |
| `PATCH /admin/users/{id}` (`UpdateAdminUser`) | `PATCH /api/v1/admin/users/{id}` | 200,400,401,403,404,409 → 200 E; merge patch; ETag/If-Match; last-admin stays 409 | N | plan PUT, self GET (new) | W; C | U |
| `GET /admin/plans` (`GetAdminPlans`) | `GET /api/v1/admin/plans` | 200,401,403 → 200 E; array with per-plan ETags | C; no field filters | update PATCH per plan | W; C | T |
| `PUT /admin/plans` (`UpdateAdminPlans`) | `PATCH /api/v1/admin/plans/{id}` | 204,400,401,403 → 204 → 200 E; one plan merge patch; batch fan-out D3; ETag/If-Match | N; id remains existing numeric plan id | self GET (new), users GET | W; C | T |
| `GET /admin/flags` (`GetAdminFlags`) | `GET /api/v1/admin/flags` | 200,401,403 → 200 E; array with per-flag ETags | C; no field filters | update PATCH; collection creation POST (new) | W; C | F |
| `PUT /admin/flags` (`UpdateAdminFlags`) | `PATCH /api/v1/admin/flags/{name}; POST /api/v1/admin/flags (new only)` | 204,400,401,403 → 204 → 200 E update / 201 E + L create; atomic batch D3; ETag/If-Match on update | N | self GET (new), update PATCH | W; C | F |
| `PUT /admin/users/{id}/plan` (`UpdateAdminUserPlan`) | `PUT /api/v1/admin/users/{id}/plan` | 204,400,401,403,404 → 204 B; complete assignment; ETag/If-Match | N | No body; discover from user; assignment GET needed | W; C | T |
| `GET /admin/calls/stats` (`GetAdminCallStats`) | `GET /api/v1/admin/call-reports/current` | 200,400,401,403 → 200 E; report object retains calls_per_hour, top_error_codes, latency_by_method, retries_by_facet, completed_per_hour; invalid window 400 → 422 | N; retain required window=1h or 24h; deterministic hour/outcome order within bounded report | quotas GET, queue GET | W; C | O |
| `GET /admin/ops/status` (`GetAdminOpsStatus`) | `GET /api/v1/admin/operation-reports/current` | 200,401,403 → 200 E; report object; estimates explicitly labelled, never exact pagination totals | N; snapshot aggregates are not entity lists | queue GET, quotas GET, call-report GET | W; C | O |

### Downloads and events

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key |
| --- | --- | --- | --- | --- | --- | --- |
| `POST /downloads` (`createDownload`) | `POST /api/v1/downloads` | 201,400,401,409 → 201 → 202 E + L + I; download status; skipped hidden photo count retained | N | self GET (new), parts GET, retries POST when failed, events GET | W; C | D |
| `GET /downloads/{id}/parts` (`getDownloadParts`) | `GET /api/v1/downloads/{id}/parts` | 200,401,404,500 → 200 E; parts wrapper → array of part resources | C; no field filters; stable part number order | download GET, transfer GET (raw), acknowledgement PATCH per part | W; C | D |
| `POST /downloads/{id}/parts/{partNumber}/fetched` (`markDownloadPartFetched`) | `PATCH /api/v1/downloads/{id}/parts/{partNumber}` | 204,401,404 → 204 B; merge patch fetched=true; no response envelope | N | No body; discover from part; part GET needed | W; C | D |
| `POST /downloads/{id}/retry-failed` (`retryFailedDownload`) | `POST /api/v1/downloads/{id}/retries` | 200,401,404,500 → 200 count → 202 E + L + I; retry request status and requeued count | N | self GET (new), download GET, events GET | W; C | D |
| `GET /events` (`streamEvents`) | `GET /api/v1/events` | 200,401 → 200 text/event-stream retained; handshake errors P, never wrap frames | No C; Last-Event-ID header; remove undocumented lastEventId fallback (D8) | entry point links to stream; event resource payloads link to download/crawl | W; C | E |

### Catalog

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key |
| --- | --- | --- | --- | --- | --- | --- |
| `GET /people` (`listPeople`) | `GET /api/v1/people` | 200,400,401,500 → 200 E; page wrapper → array; 400 invalid values → 422 | Existing limit/cursor → C metadata; preserve stable keyset order; no field filters | photos GET per person, self GET | W; C | B |
| `GET /people/{nsid}` (`getPerson`) | `GET /api/v1/people/{nsid}` | 200,401,404,500 → 200 E; hidden/missing/gone remain 404 | N | photos GET, contact GET if caller has interest | W; C | B |
| `GET /people/{nsid}/photos` (`listPersonPhotos`) | `GET /api/v1/people/{nsid}/photos` | 200,400,401,404,500 → 200 E; page wrapper → array; 400 invalid values → 422 | Existing limit/cursor → C metadata; preserve keyset order; no field filters | owner GET, downloads POST, photo self GET | W; C | B |
| `GET /favorites` (`listFavorites`) | `GET /api/v1/favorites` | 200,400,401,500 → 200 E; page wrapper → array; 400 invalid values → 422 | Existing limit/cursor → C metadata; preserve keyset order; no field filters | photo GET, owner GET, downloads POST | W; C | B |
| `GET /albums` (`listAlbums`) | `GET /api/v1/albums` | 200,400,401,500 → 200 E; page wrapper → array; 400 invalid values → 422 | Existing limit/cursor → C metadata; preserve keyset order; no field filters | photos GET, self GET (new album detail) | W; C | B |
| `GET /albums/{id}/photos` (`listAlbumPhotos`) | `GET /api/v1/albums/{id}/photos` | 200,400,401,404,500 → 200 E; page wrapper → array; 400 invalid values → 422 | Existing limit/cursor → C metadata; preserve keyset order; no field filters | album GET (new), photo GET, downloads POST | W; C | B |
| `GET /photos/{id}` (`getPhoto`) | `GET /api/v1/photos/{id}` | 200,401,404,500 → 200 E; hidden/missing/tombstoned remain 404; no sizes fetch on read | N | owner GET, downloads POST when available | W; C | B |

### Probes

| Current method/path (operation) | Target method/path | Current statuses → target success/body (P on all errors) | Pagination / filters | Resource links beyond self | Breaking | Consumer key |
| --- | --- | --- | --- | --- | --- | --- |
| `GET /healthz` (`getHealth`) | `GET /healthz` | 200,default → 200 E; default failure → 500 P (D6) | N | ready GET | W; C | H |
| `GET /readyz` (`getReadiness`) | `GET /readyz` | 200,503 → 200 E; 503 dependency object → 503 P (D6) | N | health GET | W; C | H |

## Browser routes and non-contract surfaces

These are safe HTML navigations, not versioned JSON API operations. Each row
covers GET and HEAD of that one browser pattern. Target is the same web path;
200 HTML/HEAD without body, or the existing client-side guard/not-found state.
No API envelope, problem body, pagination or resource links apply to the HTML
shell. W/C breaking: **no** for these page URLs; API calls made by the pages
change as indicated above. `web_assets_test.go`, `app/guards.test.tsx`,
`web/e2e/99-route-sweep.spec.ts` and router/nav declarations are evidence for
this table. The SPA fallback serves the shell before React resolves its page.

| Current browser method/path | Target browser method/path | Screen / implementation |
| --- | --- | --- |
| GET/HEAD `/login` | GET/HEAD `/login` | LoginPage, I |
| GET/HEAD `/register` | GET/HEAD `/register` | RegisterPage, I |
| GET/HEAD `/` | GET/HEAD `/` | UserShell home placeholder |
| GET/HEAD `/library` | GET/HEAD `/library` | Library placeholder, B |
| GET/HEAD `/search` | GET/HEAD `/search` | Search placeholder, B |
| GET/HEAD `/contacts` | GET/HEAD `/contacts` | Contacts placeholder, B |
| GET/HEAD `/albums` | GET/HEAD `/albums` | Albums placeholder, B |
| GET/HEAD `/crawl` | GET/HEAD `/crawl` | CrawlPage, C |
| GET/HEAD `/keys` | GET/HEAD `/keys` | KeysPage, K/A |
| GET/HEAD `/downloads` | GET/HEAD `/downloads` | Downloads placeholder, D |
| GET/HEAD `/sync` | GET/HEAD `/sync` | Sync placeholder |
| GET/HEAD `/sharing` | GET/HEAD `/sharing` | Sharing placeholder, S |
| GET/HEAD `/profile` | GET/HEAD `/profile` | ProfilePage, P |
| GET/HEAD `/contacts/:id` | GET/HEAD `/contacts/:id` | Contact detail placeholder, B/C |
| GET/HEAD `/albums/:id` | GET/HEAD `/albums/:id` | Album detail placeholder, B |
| GET/HEAD `/accounts` | GET/HEAD `/accounts` | Account placeholder, A |
| GET/HEAD `/sessions` | GET/HEAD `/sessions` | Sessions placeholder (Profile has implemented session card), P |
| GET/HEAD `/onboarding` | GET/HEAD `/onboarding` | SetupPage, K/A/C |
| GET/HEAD `/admin` | GET/HEAD `/admin` | AdminShell overview placeholder, O/Q |
| GET/HEAD `/admin/nodes` | GET/HEAD `/admin/nodes` | Nodes placeholder, N |
| GET/HEAD `/admin/nodes/:id` | GET/HEAD `/admin/nodes/:id` | Node detail placeholder, N |
| GET/HEAD `/admin/quota` | GET/HEAD `/admin/quota` | Quota placeholder, Q |
| GET/HEAD `/admin/metrics` | GET/HEAD `/admin/metrics` | Metrics placeholder, O |
| GET/HEAD `/admin/events` | GET/HEAD `/admin/events` | Events placeholder, E |
| GET/HEAD `/admin/issues` | GET/HEAD `/admin/issues` | Issues placeholder |
| GET/HEAD `/admin/pipeline` | GET/HEAD `/admin/pipeline` | Pipeline placeholder, O |
| GET/HEAD `/admin/downloads` | GET/HEAD `/admin/downloads` | Downloads placeholder, D |
| GET/HEAD `/admin/sync` | GET/HEAD `/admin/sync` | Sync placeholder |
| GET/HEAD `/admin/ai` | GET/HEAD `/admin/ai` | AI placeholder |
| GET/HEAD `/admin/users` | GET/HEAD `/admin/users` | Users placeholder, U |
| GET/HEAD `/admin/defaults` | GET/HEAD `/admin/defaults` | Defaults placeholder, T/F |
| GET/HEAD `/admin/sharing` | GET/HEAD `/admin/sharing` | Sharing placeholder |
| GET/HEAD `/admin/log` | GET/HEAD `/admin/log` | Call log placeholder, O |
| GET/HEAD `/*` | GET/HEAD `/*` | UserNotFound fallback |
| GET/HEAD `/admin/*` | GET/HEAD `/admin/*` | Admin UserNotFound fallback |
| GET/HEAD `/assets/*` and dotted static-file paths | Same paths | FileServer assets; own MIME, HEAD empty, ordinary static 404; not a JSON API |

**OAuth return pages:** the current Flickr callback is a backend GET in the
55-row mapping, not an implemented React return page. Browsers currently get
302 to `/keys?connected=1` or `?connect_error=...` from the adapter; JSON callers
get the contract response. Replace that side effect with a safe return page
at `/flickr-accounts/callback`, then a CSRF-protected PUT. This page must be
allowed through the current `isBackendOnlyPath` exclusion; it must not be
swallowed by a wildcard not-found screen. Refreshing the HTML page must not
exchange the verifier again. No additional social OAuth return page is mounted
in `router.tsx` yet. The documented social callback and `/register/social`
completion page below are provisional, not hidden existing operations.

No standalone HTTP ZIP transfer route is registered today. The local BlobStore
returns `file:` URLs (`media/adapters/local/blobstore.go:PresignGet`), which
cannot satisfy a browser HTTP transfer. The S3 adapter returns external
presigned URLs governed by that provider. JOOXF-316/download work must supply a
safe HTTP transfer link for local storage (D4); do not count file URLs as
existing product HTTP operations. Unknown API routes must return 404 P, and
API prefixes must bypass the SPA HTML fallback even with `Accept:text/html`.

## (a) Already conforming routes and conforming aspects

**Fully conforming API operations: none (0/55).** Every public product API
except the probes lacks `/api/v1`; heartbeat lacks `/internal/v1`. JSON schemas
also lack the required success envelope and resource links. Probes preserve
their unversioned URLs, but their current bare JSON and readiness 503 JSON are
not an exception to rules 4/5/9. D6 asks whether the lead wants such an exception;
this mapping follows the unchanged rule until then.

The 35 explicit browser page/fallback patterns and static asset surface above
are outside the JSON API rules and keep their addresses. They are not part of
the 55-operation denominator. Existing conforming *aspects*, not whole routes:

- `/healthz` and `/readyz` already have the required path/method placement.
- `/events` already has safe GET, 200 `text/event-stream`, event IDs and
  `Last-Event-ID`; only its prefix and error/linked payload contract need work.
- Logout, session revocations and key deletion already have empty 204 success
  and no DELETE request body (logout still has the wrong method/path).
- Catalog collections already use opaque keyset cursors with 50/100 limits;
  move pagination into the common metadata and add links without extra Flickr
  calls. Existing hidden detail 404 and list visibility filtering remain.
- User, key and node creation already return 201, but need Location and linked
  envelopes. Download creation must use 202 because packing outlives the request.
- `GET /me`, sharing PUT and safe catalog noun paths have suitable method/resource
  semantics before adding the version prefix and common representations.

## Supporting resources required by the target

These are proposed **new** operations, not missing existing inventory rows.
They make Location, self, related and action links usable. Use E/P/C/B and
cookie/bearer policy exactly as above; no new business capabilities are implied.

| Module | New method/path | Required representation / purpose |
| --- | --- | --- |
| Foundation | `GET /api/v1` | 200 E entry point; permission-filtered collection links, me, events, authentication options. |
| Identity | `GET /api/v1/users/{id}` | 200 E permitted public user projection for registration Location; hide other users unless explicitly permitted; alternative redirect to me requires D4. |
| Identity | `GET /api/v1/sessions/{id}` and `GET /api/v1/sessions/current` | 200 E owner-scoped session, status and revocation; never return token hashes/cookies. |
| Identity | `GET /api/v1/me/sessions/{id}` | 200 E owner session self for paged members; same domain read as sessions, no duplicate business validation. |
| Identity | `GET /api/v1/me/sharing` | 200 E persisted choice for sharing self/discovery. Never return a password resource's secret; me advertises password PUT only. |
| Accounts | `GET /api/v1/flickr-authorizations/{state}` | 200 E redacted pending/completed status for self; no OAuth token/verifier. Completion PUT returns 200 E of this same authorization, linking to account. |
| Accounts | `GET /api/v1/flickr-accounts/{id}` | 200 E owner account detail for member self/completion links. |
| Crawl | `GET /api/v1/me/contacts/{nsid}` | 200 E caller's starred/selected/paused state; 404 for absent interest. |
| Admin | `GET /api/v1/admin/users/{id}` | 200 E authorized admin projection and ETag. |
| Admin | `GET /api/v1/admin/users/{id}/plan` | 200 E current assignment and ETag for conditional replacement. |
| Admin | `GET /api/v1/admin/plans/{id}` | 200 E plan and ETag. Keep existing numeric plan id. |
| Admin | `GET /api/v1/admin/flags/{name}` | 200 E flag and ETag. Flag name remains its existing identifier. |
| Nodes | `GET /api/v1/admin/nodes/{id}` | 200 E node public projection, no stored/creation token; registration Location. Existing node name is the opaque id, not a new numeric id. |
| Nodes | `GET /internal/v1/nodes/{id}` and `GET /internal/v1/nodes/{id}/heartbeat` | 200 E node-token-scoped self/report; do not leak public admin URLs to a token that cannot follow them. |
| Downloads | `GET /api/v1/downloads/{id}` | 200 E owner-only status, parts/retries/events; 404 hidden. |
| Downloads | `GET /api/v1/downloads/{id}/retries/{retryId}` | 200 E owner-only retry status; 404 hidden. This stays within two child levels. |
| Downloads | `GET /api/v1/downloads/{id}/parts/{partNumber}` | 200 E part status, safe transfer and acknowledgement links, TTL; no local paths. |
| Downloads | `GET /api/v1/downloads/{id}/parts/{partNumber}` with ZIP Accept (proposal D4) | Raw `application/zip`, Content-Disposition; no E; P before streaming; enforce owner/expiry; same URI resource, negotiated representation. |
| Catalog | `GET /api/v1/albums/{id}` | 200 E visible album detail/count and photos relation; 404 absent/hidden. |

For favorite members, the wrapper must also have a self link distinct from the
embedded photo self link. Add an owner/visibility-scoped
`GET /api/v1/favorites/{favoriteId}` (200 E/P). D4 includes choosing its stable
opaque identity from the existing favorite relationship, without changing photo
numeric IDs or leaking account ownership. Do not substitute the photo URL as
the favorite relationship self. Photo sizes are embedded value descriptions
with external media URLs; if promoted to API resources, they also need
addressable self links.

For aggregate quota members, `self` needs a routable admin projection, proposed
`GET /api/v1/admin/key-quotas/{id}` (200 E/P). Do not point an admin to an
owner-only `/keys/{id}/quota` they cannot read. Fixed snapshot subobjects such
as lane totals can be scalar value fields rather than independently identified
resources; arrays that really are resource collections must use C. GET of a
write-only credential is deliberately unavailable; expose its update relation
on the owning public resource instead of pretending the secret is readable.

## Provisional identity contracts (not currently mounted)

The starting mapping also mentions future email verification, reset, two-factor
and social flows. The UI specs call these **contract requests**; there are no
corresponding OpenAPI/router operations at this baseline. They do not inflate
the audited route count. Finish their contracts in their own feature tickets,
using these targets, E/P/L, safe GET and noun request resources.

| Documented request / surface | Proposed strict target | Response, links, pagination and affected screen |
| --- | --- | --- |
| Email verification request/completion (`ui/views/user-verify-email.md`) | `POST /api/v1/email-verifications`; `PUT /api/v1/email-verifications/{token}` | 201 E + L (or 202 + I if queued delivery is the tracked work); GET status supports self, no token readback; 200 E completion; no C; verify-email screen; expired status decision D5. |
| Password reset request/token check/completion (`ui/views/user-password-reset.md`) | `POST /api/v1/password-resets`; `GET /api/v1/password-resets/{token}`; `PUT /api/v1/password-resets/{token}` | 201 E + L or tracked async 202 + I; safe token inspection only; 200 E completion; no C; no account-existence disclosure; reset screen; D5/D7. |
| Two-factor management GET | `GET /api/v1/me/two-factor` | 200 E redacted status; enrolments/disablements/recovery-code-sets relations; no C; security management screen. |
| `POST /me/two-factor/enrol` | `POST /api/v1/me/two-factor-enrolments` | 200 → 201 E + L; pending enrolment self GET; QR/shared secret one-time delivery D7; no C. |
| `POST /me/two-factor/confirm` | `PUT /api/v1/me/two-factor` | 200 E full enabled factor state with confirmation proof; recovery-code delivery D7; no C. |
| `POST /me/two-factor/disable` | `POST /api/v1/me/two-factor-disablements` | 204 → 201 E + L disablement result, self GET; proof in body, never DELETE body; no C. |
| `POST /me/two-factor/recovery-codes` | `POST /api/v1/me/recovery-code-sets` | 200 → 201 E + L; self GET returns metadata only, codes once subject to D7; no C. |
| `POST /auth/2fa/verify`, `POST /auth/2fa/cancel` | `PUT /api/v1/session-challenges/{id}`; `DELETE /api/v1/session-challenges/{id}` | 200 E completed challenge with session link/cookie; 204 B cancellation; safe GET for challenge status; no C; login challenge screen; D5. |
| `GET /auth/social/{provider}/start` | `POST /api/v1/social-sign-ins` | 201 E + L pending sign-in with external authorize link; provider/mode/return_to in body; no C. |
| `GET /auth/social/{provider}/callback` | Safe web return page then `PUT /api/v1/social-sign-ins/{state}` | HTML GET exchanges nothing; 200 E completion status with session or registration-ticket relation; no C; same provider-neutral return screen; planned path needs contract agreement. |
| `POST /auth/social/complete`, `/register/social` | `POST /api/v1/users` with one-time sign-up ticket; keep `/register/social` HTML page | 201 E + L; account/user link; preserve verified-email and terms rules; no C. |
| `DELETE /me/sign-in-methods/{provider}` | `DELETE /api/v1/me/sign-in-methods/{provider}` | 204 B; collection/status GET supplies self and unlink relation, C if listing methods; preserve last-method 409; Profile. |

Social linking must keep authenticated `mode=link`, nonce/PKCE, no email-based
auto-link, and the existing audit events. Provisional passkey paths mentioned
only by mocks are not implemented routes and need a separate feature contract;
this audit does not invent authentication behavior.

## (b) Decisions the rules do not settle

The lead finalized D1–D8 for this delivery split. The audit notes below retain
their original rationale; they are not pending questions. This identity slice
applies the final decisions relevant to its routes.

1. **D1 — Pause scope mismatch.** Starting mapping says POST pause/resume →
   PATCH `/me/crawl` with `paused`. Actual contract/router say **PUT**, body
   `{nsid}`, and `crawl/app/control.go` changes a single user's contact.
   A caller-wide boolean loses that address and changes business behavior.
   Recommendation: retain contact-level behavior as PATCH
   `/api/v1/me/contacts/{nsid}` with `paused`, beside star/select. Ask the lead
   to correct the starting mapping explicitly; until then the table preserves
   its binding `/me/crawl` target and marks this blocked semantic choice.
   Do not silently introduce whole-account pause.
2. **D2 — Seven-day selection.** Current select has no request body and computes
   seven days server-side. The starting target accepts `selected_until` but
   does not state whether arbitrary dates, null to deselect, or an extension
   are allowed. Recommendation: keep the seven-day business rule, compute the
   permitted bound on the server and validate submitted timestamps; reject
   invalid values with 422. Lead must define clock/extension/deselection
   semantics before a client constructs this timestamp. Avoid a newly named
   action endpoint as a workaround.
3. **D3 — Atomic plan/flag batches.** Current bulk PUTs are partial upserts;
   omitted definitions remain, and flags are explicitly atomic. Recommendation:
   individually addressed conditional PATCH operations, and flag creation POST,
   as shown. Existing numeric plan IDs stay numeric. A page making multiple
   calls is not an atomic batch; lead must approve losing atomicity or define
   a noun resource such as a configuration revision with full PUT semantics
   and ETag. Never relabel a partial bulk upsert as full replacement.
4. **D4 — Addressable status and raw transfers.** Rules require self and usable
   Location, but current API lacks user/session/authorization/download/part/
   retry/album/favorite reads. Recommendation: add the minimal supporting reads above,
   no speculative search or CRUD features. For local ZIPs use negotiated GET
   on the existing part resource with ZIP media type and Content-Disposition;
   a separate noun child transfer is also possible. Lead should choose the
   representation and session/user/favorite canonical URI before client generation.
   Preserve owner-only expiry and never expose `file:` links to browsers.
5. **D5 — Existing 423/410 states.** Final decision: configurable login
   lockout returns 429 with `Retry-After` and `/problems/account-locked`; 410
   and 423 are not used. Expired one-time flows return 409 with a stable
   expired problem type and restart relation. Identity login implements the
   429 lockout response; later reset and verification flows apply the same
   decision when delivered.
6. **D6 — Probe bodies.** Only unversioned placement is exempted. Current
   health/readiness have bare JSON, and unready 503 is a Readiness success-like
   shape. Recommendation: keep paths/status semantics and use E/links for
   healthy responses, P with safe dependency fields for 503. If operational
   tooling needs the old probe payload, a narrowly documented probe-body
   exception needs a lead rule change. A status-only uptime check needs none.
7. **D7 — One-time secrets.** Existing node creation returns a bearer token
   once; rule 8 says tokens are never returned, but singles out access tokens
   *after creation*. Provisional 2FA must also deliver enrolment secrets and
   recovery codes once. Recommendation: explicitly permit a no-store,
   authenticated one-time issuance response for these secret classes, never
   GET/readback/logging/links. Do not silently remove node bootstrap or claim
   token delivery already complies with an ambiguous rule.
8. **D8 — SSE resumption and settings.** The generated route declares only
   Last-Event-ID, while the standalone handler accepts `lastEventId`. The
   existing per-instance bounded replay history does not promise replay across
   nodes/restarts. Recommendation: retain the mandated header and explicitly
   remove the undocumented query fallback; use a fetch-stream client when a
   browser must supply the header. Do not promise durable replay without a
   separate decision/ticket. Heartbeat/history/rate-limit policies must be
   admin configurable, not new hard-coded constants in the redesign.

Naming choices such as `authentication-options`, `queue-snapshots/current`,
`operation-reports/current` and `call-reports/current` are recommendations for nouns,
not additional business behavior. Existing parent-scoped singleton nouns
(`quota`, `plan`, `heartbeat`, crawl `settings`) identify a representation of
that parent, not singular collections; caller-owned singleton roots use `/me`.
The lead should settle their final canonical names when writing the contract;
there is no authorization here to add a verb exception or exceed two child
resource levels.

## (c) Proposed JOOXF-316 delivery split and dependencies

These are proposed sub-tasks, not tickets created by this audit. Each module
slice includes its contract, generated Go/TS output, adapters and all its actual
web consumers/tests in the same delivery. It is independently reviewable and
shippable after its dependencies; never deploy the new backend with an old
client for that module. Keep numeric IDs as-is and treat all client IDs as
opaque. **Remove each old route in the same release as its replacement; no
aliases, redirects or dual API support.** Web browser paths stay as above.
The initial unversioned → v1 cutover is the owner's coordinated migration;
future breaking changes inside published v1 require v2 (rule 1).

| Order / proposed slice | Module and bounded outcome | Dependencies / decisions | Generated client and screen work required |
| --- | --- | --- | --- |
| 0 — Common REST contract | Entry point, E/P schemas, problem registry, link relations, cursor codec/metadata, merge patch, conditional writes, idempotency storage and discovery helpers. Add reusable conformance cases with JOOXF-317. Do not globally wrap old responses before their consumers migrate. | None; settle D4 canonical reads; shared admin defaults remain configurable. | Regenerate shared models and teach mutator/validator to recognize new operations; preserve old operations' decoding until each is removed. Contract-first safe base for all slices. |
| 1 — Identity | users/sessions/me/password/sharing, public authentication options, conditional profile where relevant, minimal owner reads. Session auth/CSRF remain; token scopes coordinated with JOOXF-318. Provisional reset/2FA/social are separate feature work, not silently included. | 0; D5 and later D7; sharing adapter coordinated with its module. | Regenerate default/sharing client; Login, Register, Profile, SessionProvider, shells, Maintenance error handling, mocks, auth/profile E2E. |
| 2a — Keys | Versioned key CRUD, PATCH update, masked public DTOs, cursor list, key self/action links; preserve one echo and key.deleted. | 0, 1; quota relation supplied by 2c. Do not advertise a relation before it exists. | Regenerate keys client; AddKey dialogs, Keys, Setup, banners; keys MSW/E2E. |
| 2b — Accounts | Flickr authorization request/completion/status and account reads; safe browser return page. | 0, 1, 2a; D4; update configured callback URL with the shipped return page. | Regenerate accounts client; Keys/Setup startConnect 201 handling, callback page, navigation, banners; OAuth E2E with fake provider. |
| 2c — Quota | Key quota and admin per-key quota projection/list; snapshot metadata, cursor, units and permitted links. | 0, 1, 2a; can ship before 2b. | Regenerate keys/admin clients; KeyCard/loadUsage, admin shell loadAdminShell, quota mocks; admin quota planned view. |
| 3a — Crawl | Crawl/status/settings/queue and contact PATCH controls, conditional settings, no sizes read calls. | 0, 1, 2a; D1/D2; events relation after 4b. | Regenerate crawl client; Crawl, Setup depth write, contacts planned controls, query keys and crawl E2E. |
| 3b — Catalog | Versioned people/photos/albums/favorites, missing album read, visibility-preserving DTOs, cursor and member/action links. | 0, 1; download relation after 4a. | Regenerate catalog client; update direct HTTP E2E helpers now; Library/Contacts/Albums remain planned unless separately assigned. |
| 3c — Admin | Users/plan assignment, plan/flag reads and writes with ETag/If-Match, field-named email filter. | 0, 1; D3; may ship in parallel with 3a/3b. | Regenerate admin client and mocks; admin User/Defaults screens are planned; explicit contract tests for stale writes and last admin. |
| 3d — Nodes | Versioned admin registration/read and internal PUT heartbeat/report; isolate internal listener and bind path id to token. | 0, 1; D4/D7. Coordinate external node agents with server release. | Regenerate admin/internal types; node HTTP integrations; planned Nodes screens; no claimed existing node heartbeat HTTP client in this checkout. |
| 3e — Ops | Queue snapshots, call report and operation report; probe bodies if approved. | 0, 1, 2c; D6; aggregate counts remain clearly estimated where applicable. | Regenerate admin/common client, Maintenance and probe contract checks, ops tests; Overview/Metrics/Pipeline planned. |
| 4a — Downloads | Async create/retry request/status, owner reads, paged parts, PATCH acknowledgement, local HTTP ZIP representation. | 0, 1, 3b; D4; domain scheduling behavior unchanged. | Regenerate downloads client; direct callers/integration tests and planned Downloads/Tray workflow; test replay spends no duplicate Flickr call. |
| 4b — Events | Versioned SSE, problem handshake, resource links in event data and configurable transport policy. | 0, 1; D8. Coordinate producer links with 3a/4a; can migrate transport earlier without advertising absent relations. | Regenerate stream contract; implement/migrate fetch-stream consumers only when their feature is assigned; SSE tests must cover Last-Event-ID and unchanged frames. |
| 5 — Cross-module cutover proof | Run JOOXF-317 conformance across completed modules, route-access parity, generated-stale check, old-route 404, cookie vs bearer, CSRF, entry-point permissions and link-following. | All implemented module slices; every D decision affecting those slices answered. | All generated output matches contract; web consumer/validator/mocks/E2E migration complete; CLI scripts have explicit migration notes. |

A slice's release acceptance includes following every advertised link, 201/202
Location dereference, 204 empty body, safe GET/no provider calls, malformed 400
versus semantic/unknown-parameter 422, problem types, 401/429 headers, cursor
stability, hidden-resource 404, idempotency and conditional 412 as relevant.
Do not publish links into modules not shipped yet. Foundational helpers can ship
without changing old routes; each later module moves atomically with its client.
JOOXF-316 does not authorize adding future UI features merely because a nav
placeholder or provisional contract request exists.

## Verification and limits

The audit checks source registrations, schemas, adapters, generated client use,
UI route declarations and tests at the baseline above. It does not assert that
all documented future UI flows are implemented, that every existing error is
currently emitted exactly as documented, or that a live provider accepts a
callback configuration. No real credentials, production database reads, Flickr
requests, database migration or service lifecycle operation is required.

AC1 is traced to rules 1–11 in `rest-api.md` and the E/P/L/I/B/C/N definitions;
AC2 is the 55-row inventory with router parity plus the separate browser and
provisional tables. Verify completeness by resolving all contract path-item
references, extracting every HTTP method, and comparing the `(method,path)`
set to the first column of the complete mapping and `roles/routes.go`. Counts
must match and duplicates must be zero. Documentation-only changes touch no Go
or TypeScript package, so per-file unit coverage, database integration and E2E
behavioral tests are not applicable; existing automated checks and pre-push
scan results are recorded in the PR and Jira completion comment.
