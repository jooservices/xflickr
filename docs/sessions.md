# Sessions, CSRF, and roles

Authenticated API requests carry the `xf_session` cookie. Login sets the cookie
after a successful password check. The cookie is HttpOnly, Secure, and
SameSite=Lax. Its value is an opaque 32-byte id encoded with base64url. The
`sessions` table stores the SHA-256 hash, the owner, and the expiry.

A session lasts 14 days from its last accepted use. When a request arrives at
least a minute after the previous expiry write, the server moves the expiry
forward by another 14 days and refreshes the cookie. Requests inside that
minute reuse the stored expiry, so an active client does not rewrite the row
on every call.

Unsafe methods are POST, PUT, PATCH, and DELETE. Each of them must echo the
`xf_csrf` cookie in the `X-CSRF-Token` header. The comparison is exact. Safe
methods issue `xf_csrf` when it is missing. That cookie is readable by the
browser, Secure, and SameSite=Lax. A missing or mismatched token responds with
403 and code `csrf_rejected`.

`RequireUser` resolves `xf_session` to an active user and stores that principal
on the request. A missing, unknown, expired, or disabled session responds with
401 and code `unauthenticated`. `RequireAdmin` uses the same check, then
responds with 403 and code `forbidden` when the role is not `admin`.

`GET /me`, `DELETE /auth/sessions`, and `/keys` require a user. The delete removes every
other session for that user and leaves the calling session in place. Logout
revokes the calling session and clears both cookies.
