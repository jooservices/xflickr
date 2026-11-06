# REST API rules

The XFlickr API is the single door to every operation: the web client, scripts
and the MCP server all use it, and anything a user can do in the UI is available
through it. This document is the binding rule set for every route, for the
OpenAPI contract (`contracts/openapi`), for the conformance test (JOOXF-317) and
for the MCP tools (JOOXF-323 to 326).

It applies the Workspaces HTTP API standard
(`.claude/skills/http-api-design/references/standard.md`) in full and tightens
it. That standard calls an API "strict REST" only when it also implements
REST's hypermedia constraint, so XFlickr adds hypermedia links to every
resource (rule 9). The owner's requirement is strict REST; where this document
and an older route disagree, this document wins and the route changes.

## 1. Base path and versioning

- Every API route lives under `/api/v1`. A breaking change ships as `/api/v2`
  with a documented migration; never repurpose a version.
- Outside the versioned API stay only: the unversioned probes `/healthz` and
  `/readyz`, the node-only internal listener (`/internal/v1`), and the browser
  routes of the web app itself (for example the OAuth return page).
- `GET /api/v1` is the entry point. It returns links to the top-level
  collections the caller may use.

## 2. Resources and URLs

- URLs name resources with plural, lower-case, hyphenated nouns:
  `/api/v1/downloads`, `/api/v1/flickr-accounts`. A singleton owned by the
  caller sits under `/me` (`/me`, `/me/sharing`, `/me/crawl`).
- No verbs in paths. An action is modelled as creating or changing a noun:
  a password reset is `POST /password-resets`, a retry is
  `POST /downloads/{id}/retries`, a revocation is `POST /…/revocations`. There
  are no operation endpoints; the exception list is empty and a new entry needs
  a lead decision recorded here.
- Nest at most two levels, only when the parent is part of the address
  (`/downloads/{id}/parts/{number}`). Identifiers are opaque in clients.
- Filter, sort and page with query parameters named after fields. Sort is an
  allowlist (`sort=-created_at`). Unknown parameters are rejected with 422.

## 3. Methods

- `GET` is safe and never changes state. Add `ETag` where a validator is cheap.
- `POST` creates a member of a collection, including a request resource that
  starts work. It returns `201` with `Location`, or `202` for asynchronous work
  (rule 7).
- `PUT` replaces the addressed resource with the full representation and is
  idempotent. `PATCH` uses JSON Merge Patch (`application/merge-patch+json`) for
  partial changes, for example `PATCH /me/crawl` with `{"paused": true}`.
- `DELETE` is idempotent, has no request body, and returns `204`.
- Resources that two people can edit (settings, plans, flags, defaults) send an
  `ETag` and require `If-Match` on `PUT` and `PATCH`; a stale one is `412`.

## 4. Success responses

- JSON successes use the standard envelope, exactly:
  `success`, `code` (equal to the HTTP status), `message`, `data`, `meta`,
  `errors` (null on success) and `trace_id`.
- `204` has no body. Raw files (a ZIP part) are returned with their own media
  type and `Content-Disposition`, without the envelope.
- `data` is an explicit public representation, never a database row. Timestamps
  are RFC 3339 in UTC; durations and sizes name their unit in the field.

## 5. Errors

- Errors are `application/problem+json` (RFC 9457) with `type`, `title`,
  `status`, `detail`, `instance`, `trace_id`, and `errors` for field problems
  (field name to an array of safe messages).
- `type` is a stable `/problems/<slug>` URI; existing machine codes become
  slugs (`key-invalid`, `temp-storage-full`, `reconnect-required`). Clients
  branch on `type`, never on `title` or `detail`.
- Status use: `400` malformed, `401` unauthenticated (with `WWW-Authenticate`),
  `403` authenticated but not allowed, `404` absent or hidden (another user's
  resource is `404`, never `403`), `409` state conflict (for example the last
  active administrator), `412` failed precondition, `422` valid syntax with
  invalid values, `429` rate limited (with `Retry-After`).
- Never expose stack traces, SQL, secrets or internal hosts.

## 6. Collections and pagination

- Collections return `data` as an array and use opaque cursor pagination:
  `limit` (default 50, maximum 100) and `cursor`. `meta.pagination` carries
  `limit`, `has_more` and `next_cursor`; `meta.links` carries usable `self` and
  `next` URLs. Ordering is stable and deterministic. No total count unless it is
  exact and cheap.

## 7. Asynchronous work and idempotency

- Work that outlives the request is a resource with a `status` field. The
  `POST` returns `202` and `Location` of that resource; clients poll it or follow
  the event stream (`GET /api/v1/events`, `text/event-stream`, resumable with
  `Last-Event-ID`). Examples: a download, a cloud sync, a reindex.
- `POST` that creates such work accepts `Idempotency-Key`; the same key within
  24 hours returns the first result instead of repeating the work.

## 8. Authentication and authorization

- Browser: the `xf_session` cookie with the double-submit CSRF token on every
  unsafe method. Scripts and the MCP server: `Authorization: Bearer <access
  token>` with scopes (JOOXF-318). A request uses one mechanism, never both; CSRF
  applies to cookies only.
- Every operation checks object-level and property-level permission. The
  sharing and visibility policy applies to every read. Secrets (Flickr keys and
  secrets, tokens, access tokens after creation) are never returned.
- Rate limits are per caller, configurable in the admin defaults, and answer
  `429` with `Retry-After`.

## 9. Hypermedia

- Every resource in `data` carries `links`: an object whose members are
  `{"href": "...", "method": "..."}`. `self` is always present. Related
  collections and the actions currently possible appear as links
  (`downloads`, `parts`, `retries`). A link that is absent means the action is
  not available now, so clients follow links instead of building URLs.
- Collections link to their members and to `next`; the entry point links to
  everything else. Link relation names are documented in the OpenAPI contract.

## 10. Third-party callbacks

- A redirect from an outside provider (Flickr, a social provider) cannot be a
  state-changing `GET`. The provider returns to a page of the web app, and that
  page completes the flow with a normal call, for example
  `PUT /flickr-authorizations/{state}`.

## 11. Contract and tests

- `contracts/openapi` is the source of truth and is written before or with the
  implementation; the Go server stubs and the TypeScript client are generated.
- The conformance test (JOOXF-317) reads the contract and fails the build on a
  verb in a path, a missing envelope or problem type, a `GET` that is not safe,
  a `DELETE` with a body, a missing `Location`, a collection without pagination,
  or a resource without `links`.
- Each MCP tool and each UI operation maps to exactly one documented API
  operation.

## Starting target mapping

The routes below are known to break the rules. JOOXF-314 maps every route; the
methods of today's routes are verified there.

| Today | Target |
| --- | --- |
| `POST /auth/register` | `POST /api/v1/users` |
| `POST /auth/login` | `POST /api/v1/sessions` |
| `POST /auth/logout` | `DELETE /api/v1/sessions/current` |
| `DELETE /auth/sessions` (others) | `DELETE /api/v1/me/sessions?scope=others` |
| `POST /flickr-accounts/connect` | `POST /api/v1/flickr-authorizations` (returns the Flickr URL) |
| `GET /flickr-accounts/callback` | web page, then `PUT /api/v1/flickr-authorizations/{state}` |
| `POST /me/crawl/pause` and `/resume` | `PATCH /api/v1/me/crawl` with `{"paused": true}` or `false` |
| `/me/contacts/{nsid}/star` and `/select` | `PATCH /api/v1/me/contacts/{nsid}` with `starred` or `selected_until` |
| `POST /downloads/{id}/retry-failed` | `POST /api/v1/downloads/{id}/retries` |
| `POST /downloads/{id}/parts/{n}/fetched` | `PATCH /api/v1/downloads/{id}/parts/{n}` with `{"fetched": true}` |
| `POST /internal/nodes/heartbeat` | `PUT /internal/v1/nodes/{id}/heartbeat` |
| email verification (provisional) | `POST /api/v1/email-verifications`, `PUT /api/v1/email-verifications/{token}` |
| password reset (provisional) | `POST /api/v1/password-resets`, `GET` and `PUT /api/v1/password-resets/{token}` |
| two-factor (provisional) | `POST /api/v1/me/two-factor-enrolments`, `PUT /api/v1/me/two-factor`, `POST /api/v1/me/two-factor-disablements`, `POST /api/v1/me/recovery-code-sets` |
| social sign-in (provisional) | `POST /api/v1/social-sign-ins`, then `PUT /api/v1/social-sign-ins/{state}` |
