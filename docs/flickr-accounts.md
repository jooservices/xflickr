# Flickr account connections

A user connects one or more Flickr accounts through one of their stored API
keys. The access token belongs to the key that authorized it: deleting the key
deletes its accounts.

## Flow

1. `POST /flickr-accounts/connect` with `{key_id}` (signed in, CSRF token
   required). The key must belong to the user and be `valid`. XFlickr asks
   Flickr for a request token with that key and returns the Flickr authorize
   URL (permission `read`).
2. The request token is kept in Redis for 10 minutes under the worktree prefix.
   The Redis key is the SHA-256 of a random 256-bit `state`; the request token
   secret is sealed, bound to that state, so Redis never holds it in plaintext.
3. The member approves on Flickr. Flickr redirects the browser to the callback
   URL from `FLICKR_OAUTH_CALLBACK_URL`. That URL carries the `state` query
   parameter that step 1 added to it.
4. `GET /flickr-accounts/callback?state&oauth_token&oauth_verifier` completes
   the flow. The state is read and deleted in one step, so it works once even
   if a later step fails. It must belong to the signed-in user and the returned
   `oauth_token` must be the request token that was issued; otherwise the
   response is `invalid_state` (400). This is the CSRF defense: a forged
   callback cannot attach an attacker's Flickr account to another user.
   A missing verifier means the member declined (`authorization_denied`, 400).

## Storage

`flickr_accounts(id, user_id, key_id, nsid, username, token_sealed,
secret_sealed, status)` with a unique `(user_id, nsid)`. The token and token
secret are sealed with platform envelope encryption and the associated data
`flickr_accounts:<id>:token` and `flickr_accounts:<id>:secret`. No endpoint
returns them. Connecting an nsid the user already has keeps one row: the key,
username and tokens are replaced and the status becomes `ok`.

## Status

`ok`, `reconnect_required`, `revoked`.

- Changing the secret of a key sets its `ok` accounts to `reconnect_required`.
- A Flickr auth error (`ErrTokenRevoked`, codes 98 and 99) reported through
  `Connect.ReportCallError` sets the account to `revoked` and publishes
  `account.revoked` once. Repeating the report changes nothing.
- Authorizing again sets `ok` from either state.

## Events

Published to the transactional outbox in the same transaction as the change.
Payloads carry no token material.

- `flickr_account.connected` — `account_id`, `user_id`, `key_id`, `nsid`,
  `username`, `interest: "person"`, `depth: 0`, `reconnected`. The connected
  account is registered as a depth 0 person interest; crawl planning consumes
  the event.
- `account.revoked` — `account_id`, `user_id`, `key_id`, `nsid`.

## Configuration

`FLICKR_OAUTH_CALLBACK_URL` (default `http://127.0.0.1:8080/flickr-accounts/callback`)
must be an absolute http or https URL that the member's browser can reach.
The Flickr API key and secret are entered only in the UI.
