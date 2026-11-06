# REST building blocks

Slice 0 adds `internal/platform/api/rest` without migrating existing handlers.
`GET /api/v1` is the additive discovery entry point. Its `data.links` includes
`self` and public authentication options. A valid session adds keys, Flickr
accounts, people, albums, favorites, me and events; administrators also see users, plans
and flags. Targets point to existing, usable routes during migration. No link
advertises an unimplemented versioned collection. Discovery is private and
uncacheable, rejects query parameters with 422 and makes no Flickr requests.
Invalid sessions return a 401 problem and authentication challenge.

## Responses and inputs

- `WriteSuccess`, `Created`, `Accepted` and `NoContent` implement the envelope,
  Location and empty-body rules. Supply explicit public DTOs and the trusted
  tracing context's trace ID. Serialization errors occur before headers commit.
- `NewProblem` resolves stable problem slugs, titles and statuses. `WriteProblem`
  adds the request instance and trace ID and uses `application/problem+json`.
  Supply safe details and `FieldErrors` (field to message arrays). Unknown
  slugs fail closed to a safe 500. Set a meaningful `Retry-After` when returning
  account-locked or rate-limited; the helper does not invent a retry duration.
- `NewLink` supports an optional method; `Links` represents self and relations.
- `ParseLimit` defaults to 50 and rejects values outside 1–100 with 422.
  `EncodeCursor`/`DecodeCursor` use strict base64url as an opaque transport
  codec, not encryption or authorization. The repository defines and validates
  its cursor payload, filters and stable unique ordering. `PageMeta` preserves
  filters and supplies `meta.pagination` and real self/next URLs. An empty
  next cursor means the page is final; list DTOs must use non-nil arrays.
- `ReadMergePatch` requires `application/merge-patch+json`, a bounded JSON
  object, and an explicit writable top-level field allowlist. It preserves
  null and raw nested values; the resource owns nested schema validation and
  RFC 7396 merge application. Malformed syntax is 400, unknown fields 422,
  unsupported media type 415. Never include server-owned fields in the list.
- `ETag` hashes the public representation into a strong validator.
  `RequireIfMatch` accepts a matching strong tag or `*` for an existing
  resource, and returns 412 for missing/stale/weak validators. The caller must
  compare the version and apply the update atomically in its persistence layer.

## Idempotency

`Idempotent` uses an `IdempotencyStore` port. `rest/postgres.Store` implements
it with a primary key on `(principal, key)` and an expiry index. Scope principal
to the authenticated identity. `Fingerprint` binds exact body bytes, method,
request target (including query) and media type, preventing reuse across
operations. Keys above 255 bytes and missing identities are rejected with 422;
a different request for an existing key returns 409. A missing key executes
normally. Retention is the single `IdempotencyRetention` constant (24 hours).

Claims are committed before work starts. Concurrent duplicates cannot both
execute: a pending claim returns 409, and a completed claim replays its original
status, headers and bytes. A callback error, process crash, or failed response
save keeps the claim pending until expiry rather than repeating uncertain
side effects. Durable work resources should reconcile pending operations in
their own application layer. Callbacks must return the complete response,
including Location and safe problem responses, to make them replayable. Do not
use this store for one-time secret issuance or persist sensitive headers.
Expired keys are replaced atomically on reuse; expiry rows are not otherwise
purged by this slice. No worker or administrator retention setting is added.
The claim generation is checked when saving so an old worker cannot overwrite
a newer claim. No existing POST is wrapped by this slice.

## Verification and reuse

`backend/internal/platform/api/rest/testdata/conformance.json` is a reusable
table of status, media type, body and Location expectations for JOOXF-317.
`TestConformanceCases` executes that table against the foundation. Additional
unit tests cover registry fields, pagination, strict patches, conditional writes,
replay, conflicts and failure paths. PostgreSQL integration tests prove atomic
claims, replay, principal isolation and expired-key replacement. Router tests
cover anonymous/user/admin discovery and retain old route access parity.
The OpenAPI entry response and shared schemas generate Go and TypeScript;
web response validation opts in only `/api/v1` and preserves legacy decoding.
