# Social sign-in and account linking

Users can sign in or create an account with Google, Facebook, X or Apple, and
link or unlink those accounts in Profile. The design is provider-neutral: the
same screens and the same rules serve every provider, and a fake provider drives
every automated test. Real provider applications are not registered in release
1. The UI specification is
[`ui/views/user-auth.md`](ui/views/user-auth.md) for sign-in and
[`ui/views/user-profile.md`](ui/views/user-profile.md) for linking.

## Provider-neutral model

- A provider is configuration, not a branch in the UI. `GET /auth/config`
  returns `providers[]` with `id` (stable lowercase, for example `google`),
  `name` (display name), and `enabled`. The sign-in group renders the enabled
  providers in the server order and hides when the list is empty.
- A provider adapter returns an identity `{provider, subject, email,
  email_verified, display_name}`. `subject` is the provider's stable user id.
  Email is never the identity key.
- The UI has one `Continue with {provider}` action, one `Link {provider}`
  action, and one provider row treatment. No provider-specific copy keys,
  routes, icons or error handling exist outside the configured name and the
  neutral initial mark.
  Official provider marks and brand rules belong to the build ticket.
- Tests use a `fake` provider adapter with scripted outcomes. No test calls a
  live provider, and the fake is the only provider enabled in the test
  environment.

## Flows

1. **Sign in.** The person chooses `Continue with {provider}` on `/login` or
   `/register`. The browser leaves the single-page app with a top-level
   navigation to `GET /auth/social/{provider}/start?return_to=...`. The server
   stores state, nonce, PKCE verifier, origin page and the validated
   `return_to` in a short-lived HttpOnly cookie, then redirects to the
   provider.
2. **Callback.** `GET /auth/social/{provider}/callback` validates state, nonce
   and PKCE, exchanges the code, and reads the identity. It never trusts an
   email without `email_verified`.
3. **Existing identity.** If the identity is already linked, a session starts
   and the browser returns to the validated `return_to`, otherwise `/`.
4. **New identity, no matching account.** When the provider returns a verified
   email that no account uses, the server issues a one-time sign-up ticket and
   redirects to `/register/social?ticket=...`. The completion screen shows the
   provider name, the verified email, a suggested display name and the terms
   checkbox. `POST /auth/social/complete` creates the account and starts a
   session, then the person continues to `/onboarding`. This preserves the
   existing rule that registration requires accepting the terms.
5. **Link.** From Profile the person chooses `Link {provider}`. The same
   start/callback flow runs with `mode=link` and a session check. On success the
   identity is attached to the signed-in account and the browser returns to
   `/profile`.
6. **Unlink.** Profile asks for confirmation and calls
   `DELETE /me/sign-in-methods/{provider}`. The current session stays signed in.

## Linking rules

- An identity belongs to exactly one XFlickr account. The database enforces a
  unique `(provider, subject)`.
- An account has at most one identity per provider.
- A provider email that matches an existing account never signs the person in
  and never links automatically. The callback answers `email_taken` and
  the UI asks the person to sign in with an existing method; linking happens
  only from an authenticated session. This is the AC2 rule of JOOXF-280.
- An identity linked to another XFlickr account answers
  `identity_taken`; nothing is moved or merged.
- Only a verified provider email can create an account. An unverified or
  missing email answers `email_unverified`; the person can use email and
  password registration instead.
- Link and unlink require an authenticated session and CSRF protection.
- Unlinking is refused with `last_sign_in_method` when the account would have
  no password, no passkey and no other linked provider. The Profile card shows
  the reason and disables `Unlink`.
- A link or unlink publishes `auth.identity.linked` / `auth.identity.unlinked` events (SSE topic `auth.identities`)
  through the transactional outbox for audit. Payloads carry no tokens.

## Screens

| Surface | Design |
| --- | --- |
| `/login` | `or` divider, a provider group (`Continue with Google`, `Facebook`, `X`, `Apple`), then the passkey option. Provider group hidden when no provider is enabled. |
| `/register` | The same provider group under `Create account`. First-time provider sign-in continues on the completion screen. |
| `/register/social` | Completion card: provider, verified email, display name, terms, `Create account`, `Use another method`. |
| `/profile` | Sign-in methods card after Passkeys: one row per configured provider, status, link or unlink action, and the last-method protection note. A link or unlink failure is a card banner. |

## Error codes

The callback redirects to the page that started the flow with
`?social=<code>&provider=<id>`. `/register/social` shows its own expired state.

| Code | Meaning | UI treatment |
| --- | --- | --- |
| `email_taken` | Provider email belongs to an existing XFlickr account | Bad banner, `Sign in instead` |
| `denied` | The person cancelled on the provider | Info banner, try again or use email |
| `email_unverified` | The provider did not confirm the email | Bad banner, use another method |
| `identity_taken` | The identity is linked to another XFlickr account | Bad banner, nothing changed |
| `state_invalid` | State, nonce or PKCE failed or expired | Bad banner, start again |
| `unavailable` | Provider disabled or misconfigured | Bad banner, try later or use email |
| `failed` | Network or provider 5xx during the exchange | Bad banner, retry or use email |
| `account_disabled` | The resolved XFlickr account is disabled | Bad banner, contact the administrator |
| `last_sign_in_method` | Unlink would leave no way to sign in | Warning in the Profile card, action disabled |

## Configuration

- Provider client ids, secrets and callback URLs are administrator settings,
  not hard-coded values. Provider secrets are sealed like other credentials and
  are never returned by an endpoint or written to logs.
- `GET /auth/config` is the only public source of provider availability. An
  empty list disables the whole social group without an error.
- The fake provider is enabled only in development and test configuration. It
  is never enabled on the develop runtime or a release.

## Testing

- Unit tests cover the provider registry, state/nonce/PKCE validation, the
  linking rules (new, existing, taken, unverified, disabled, last method) and
  the UI provider group with zero, one and four providers.
- Integration tests cover the unique identity constraint, link and unlink
  persistence, and the email-taken path. No live provider is called.
- E2E scenarios use the fake provider for sign-in, first-time registration,
  link, unlink, the last-method block, and every error banner in English and
  Vietnamese. The test hooks are listed in the two view specs.
