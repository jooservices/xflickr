# Admin user management

Administrators manage users through two endpoints under `/admin`. Both sit
behind `RequireAdmin`: no session responds with 401 and a signed-in user who is
not an administrator responds with 403. The PATCH call also needs the CSRF
header described in [sessions](sessions.md).

## Endpoints

- `GET /admin/users` lists users newest first. `q` keeps users whose email
  contains the text, ignoring case (`%` and `_` match literally). `page`
  starts at 1 and `page_size` defaults to 25 with a maximum of 100. The
  response carries `items`, `total` (across all pages), `page` and `page_size`.
  Password hashes are never returned.
- `PATCH /admin/users/{id}` changes `role` (`admin` or `user`) and/or
  `disabled`. Send at least one field. Unknown ids respond with 404 and an empty
  body with 400.

A disabled user cannot sign in and every existing session of that user stops
authenticating, because `RequireUser` rejects disabled accounts.

## Last administrator rule

The last active administrator (role `admin` and not disabled) cannot be
disabled or demoted. The call responds with 409 and code `last_admin`. The rule
lives in `domain.User.Apply`. The PostgreSQL store checks it while holding a
transaction-wide advisory lock, so two administrators demoting each other at the
same moment cannot leave the system without one.

## Audit

Every successful PATCH calls the audit port,
`audit.Recorder.Record(ctx, actor, action, target, detail)`, with actor
`user:<admin id>`, action `user.update`, target `user:<id>` and the role and
disabled values before and after. The port is defined in
`internal/platform/audit`; until the audit trail ticket (JOOXF-14) supplies the
real adapter, the API role wires `audit.Discard`.
