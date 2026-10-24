# ADR 0002: Server-side sessions

- Status: Accepted
- Date: 2026-10-25

## Context

XFlickr accounts need a request principal and a role check on protected routes.
The browser must be able to call the API without holding a reusable credential
that the server cannot revoke. Login already proves an email and password.

## Decision

- Persist sessions in PostgreSQL. The `xf_session` cookie carries an opaque id.
  The `sessions` table stores only the SHA-256 hash of that id, the user, and
  a sliding 14-day expiry.
- Slide the stored expiry at most once per minute.
- Protect unsafe HTTP methods with a double-submit CSRF token: the `xf_csrf`
  cookie and the `X-CSRF-Token` header must match.
- Resolve the principal in HTTP middleware. `RequireUser` returns 401 when the
  session is missing or unusable. `RequireAdmin` returns 403 when the
  authenticated role is not administrator.
- `DELETE /auth/sessions` revokes every session for the user except the caller.
  Logout revokes the caller's session.

## Consequences

- A stolen cookie can be revoked, and logging out on one device can leave the
  other devices signed in until `DELETE /auth/sessions`.
- Session checks read PostgreSQL on protected requests. The one-minute slide
  interval keeps that path from updating the row on every call.
- Clients obtain `xf_csrf` from a safe response before calling an unsafe method.
- The web application reads `xf_csrf` and sends it back on unsafe requests.

## Alternatives considered

- **Stateless JWT cookies:** rejected because revocation of one device, or of
  every other device, would need an extra denylist that recreates server-side
  session state.
- **Redis as the session system of record:** rejected because account sessions
  belong with the other identity rows in PostgreSQL, which is already the
  transaction boundary.
