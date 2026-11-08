# Identity REST API

Identity and sharing operations use the versioned REST API. Success responses
use the common envelope, representations expose the links for available
operations, and failures use `application/problem+json`.

| Operation | Method and path | Response |
| --- | --- | --- |
| Read public authentication options | `GET /api/v1/authentication-options` | 200 envelope |
| Register a user | `POST /api/v1/users` | 201 envelope and `Location` |
| Create a browser session | `POST /api/v1/sessions` | 201 envelope and `Location`; sets the session cookie |
| Read the current session | `GET /api/v1/sessions/current` | 200 envelope |
| Revoke the current session | `DELETE /api/v1/sessions/current` | 204; clears session and CSRF cookies |
| Read an owned session | `GET /api/v1/sessions/{id}` | 200 envelope; hidden or absent sessions return 404 |
| Read the current user | `GET /api/v1/me` | 200 envelope |
| Update the current user's profile | `PATCH /api/v1/me` | 200 envelope; locale-only JSON Merge Patch |
| List the current user's sessions | `GET /api/v1/me/sessions` | 200 envelope with cursor metadata |
| Read an owned session in the collection | `GET /api/v1/me/sessions/{id}` | 200 envelope; hidden or absent sessions return 404 |
| Revoke an owned session | `DELETE /api/v1/me/sessions/{id}` | 204 |
| Revoke every other session | `DELETE /api/v1/me/sessions?scope=others` | 204; preserves the calling session |
| Change the current user's password | `PUT /api/v1/me/password` | 204; requires the current password |
| Read the current user's sharing choice | `GET /api/v1/me/sharing` | 200 envelope |
| Replace the current user's sharing choice | `PUT /api/v1/me/sharing` | 200 envelope |

Session lists use opaque cursors, a default limit of 50, and a maximum limit of
100. Ordering is deterministic by last-seen time and session ID. The response
includes pagination metadata and usable `self` and `next` links.

Identity and sharing responses that contain user or session data are private
and use `Cache-Control: no-store`. Authentication options are public. Cookie
authenticated unsafe requests include the readable `xf_csrf` cookie value in
`X-CSRF-Token`. The `xf_session` cookie remains HttpOnly; bearer-token support
is handled separately.

The legacy `/auth/*` identity routes and root `/me`, `/me/sessions`,
`/me/password`, and `/me/sharing` routes are removed. They return 404; clients
must follow links from `GET /api/v1` and the returned representations.
