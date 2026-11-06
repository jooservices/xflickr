# Login, register and session expired

View id `user-auth`, role USER (public until signed in), light gallery theme
(dark optional). Three screens share one page frame: sign in `/login`, create
account `/register` and the session-expired variant of sign in. Baseline: the
approved [POC](../poc/xflickr-poc.html) look (brand dots, Bricolage/Figtree,
pill buttons, surface cards); the POC has no auth screen, so every decision
below is new. Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md). Mockup:
[user-auth.html](../mockups/user-auth.html). Build ticket: JOOXF-166.

## Purpose

Let a person prove who they are with an email and password, a passkey when
enabled, or a social provider (Google, Facebook, X or Apple), create an account
when registration is open, and, when a session ends while they work, sign in
again and land on the page they left. Social sign-in is provider-neutral: the
same buttons and rules serve every configured provider, and the provider list
comes from the server. The screens do not use the app shell (no tabs, tray or
notifications). Behaviour of the in-app expired modal belongs to
[user-shell](user-shell.md); this view owns the page it sends the user to.
Account linking and unlinking after sign-in belong to
[user-profile](user-profile.md).

## Route and access

- `/login`: public. `/register`: public. Both accept `?return_to=<path>`;
  `/login` also accepts `?reason=expired`.
- `/register/social?ticket=<opaque>`: public first-time social registration
  step. It accepts only a one-time ticket issued by a provider callback; the
  ticket is held in the URL for the page lifetime and never in
  `localStorage`.
- Social failures return to the page that started the flow with
  `?social=<code>&provider=<id>` (codes in [Social errors](#social-errors)).
  The page reads them once, shows the matching banner, then removes both
  parameters with `history.replaceState`.
- Already signed in: on mount `GET /me`; 200 redirects at once (replace, no
  history entry) to the validated `return_to`, else `/` (`/admin` for `role =
  admin`). 401 shows the form. Any other status shows the Error state.
  `/register/social` is the exception: a signed-in user is redirected to
  `return_to` or `/` and the pending ticket is discarded.
- `return_to` is accepted only when it starts with a single `/`, has no
  `//`, `\` or scheme, is not `/login` or `/register`, and is at most 2048
  characters. Anything else is dropped silently and `/` is used.
- Successful register never uses `return_to`: the new user goes to `/onboarding`.
- Theme: `data-role="user"`, stored choice or system preference. The EN/VI
  toggle lives in the page header (no `GET /me` yet); the choice is stored in
  `localStorage` `xf.locale` and sent as `locale` on register.
- Document titles: `Sign in · XFlickr`, `Create account · XFlickr`.

## Layout

One centred column, no scrolling on 1440 x 900 for the login form.

- Header (no border): brand (dots + XFlickr) left, EN/VI toggle and theme
  toggle right. 16 px side padding, safe-area aware.
- Desktop (>= 900 px): two columns. Left 55 %: decorative justified mosaic of
  six neutral placeholder tiles (`alt=""`, `aria-hidden`) with the tagline
  `auth.hero.title`; hidden on narrower screens. Right: the form card.
- Form card: `card` 420 px wide (full width minus 32 px on mobile), 24 px
  padding, 14 px radius, 1 px line. Top to bottom: `h1`, optional intro line,
  banner slot (expired, social result, error, rate limit, locked), fields,
  primary submit, divider "or", social provider group, passkey button (when
  enabled), footer link row.
- Login fields: Email (`type=email`, `autocomplete=username`), Password
  (`autocomplete=current-password`, show/hide toggle at the field end). Below
  the password: nothing else; there is no "forgot password" in this release
  (see Out of scope). No "remember me": sessions are always 14 days sliding.
- Register fields: Email, Password (`autocomplete=new-password`) with a live
  rules list under it, Confirm password, Terms checkbox, submit. Display name
  is not asked here; Profile sets it later.
- Password rules (same text as the API, advisory list updated while typing,
  never blocks typing): 8 to 128 characters; must not equal the email
  (checked client side only). No composition rules and paste is allowed, so
  password managers work. A rule shows a check when met, a dot when not.
- Social provider group: `role=group` labelled `auth.social.group`, one
  full-width `button` per enabled provider, in server order. Label
  `auth.social.continue` with the provider name; a provider mark sits at the
  start. The group reuses the neutral `button` component and adds no
  provider-specific component. It is hidden when `providers` is empty, so the
  page works exactly as before when social sign-in is off. Sign in and register
  share the same group and the same behaviour.
- First-time social completion (`/register/social`): the card shows `h1`
  `auth.social.register.title`, intro `auth.social.register.text` with the
  provider name, read-only `text-field` for the verified email
  (`auth.social.register.email`), `text-field` for display name prefilled from
  the provider, the terms checkbox from register, primary
  `auth.social.register.submit`, and a footer link
  `auth.social.register.back`. No password fields are shown.
- Footer link row: login "No account? Create one" (hidden when registration is
  closed); register "Already registered? Sign in".
- Components used: `button`, `text-field`, `checkbox`, `banner`, `card`. No new
  component is introduced. The rules list is plain list markup inside
  `text-field` help; the password toggle is an `icon-btn` inside the field. The
  provider mark is decorative inline content inside its button.

## Data

Short form: the page reads one public config document and posts credentials.
Fields, endpoints and contract requests are in [Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Already signed in? | `GET /me` (exists) | 200 `role`, `locale`; 401 means show form |
| Page config | `GET /auth/config` **contract request** (public, no cookie; also issues `xf_csrf`) | `registration_open` bool, `passkey_enabled` bool, `terms_url`, `privacy_url` (same-origin paths), `password_min` int (8), `password_max` int (128), `providers[]` `{id, name, enabled}` in display order |
| Sign in | `POST /auth/login` (exists) body `{email, password}` | 200 `User` (`id`, `email`, `role`, `locale`, `created_at`) and `xf_session` cookie; 401 `invalid_credentials`; 429 `rate_limited`; 400 `validation_failed` |
| Login error detail | **contract request** on `POST /auth/login` | 429 adds header `Retry-After` and `details.retry_after_seconds`; new 423 `account_locked` with `details.locked_until` (ISO time); new 403 `account_disabled` (only after a correct password) |
| Create account | `POST /auth/register` (exists) body `{email, password}` | 201 `User`; 409 conflict; 400 |
| Register detail | **contract request** on `POST /auth/register` | body adds `accept_terms` (must be true) and `locale` (`en` or `vi`); 409 splits into `email_taken` and `registration_closed`; 400 `validation_failed` carries `details.fields` `{email?, password?, accept_terms?}` each a code (`invalid`, `too_short`, `too_long`, `required`) |
| Passkey sign in | `POST /auth/passkey/options`, `POST /auth/passkey/verify` **contract request** | options: WebAuthn request options, optional body `{email}`; verify: WebAuthn assertion, 200 `User` and cookie; 401 `passkey_failed` |
| Social start | `GET /auth/social/{provider}/start?return_to=&origin=login\|register` **contract request**, top-level navigation | 303 to the provider; the server sets an HttpOnly short-lived cookie with state, nonce, PKCE verifier, origin and validated `return_to`; 404 `provider_unavailable` when the id is unknown or disabled |
| Social callback | `GET /auth/social/{provider}/callback` **contract request**, provider redirect | Validates state, nonce and PKCE; on success sets `xf_session` and redirects to `return_to`, else `/`; otherwise redirects to `{origin}?social=<code>&provider=<id>` |
| Social sign-up ticket | `GET /auth/social/pending?ticket=` **contract request** (public) | `{provider:{id,name}, email, display_name, expires_at}` for `/register/social`; 410 `ticket_expired`; 404 unknown ticket |
| Social sign-up | `POST /auth/social/complete` **contract request** body `{ticket, display_name, accept_terms, locale}` | 201 `User` and session cookie, then `/onboarding`; 400 `validation_failed` with `details.fields`; 409 `email_taken` or `identity_taken`; 410 `ticket_expired` |
| Session expiry signal | any 401 `unauthenticated` handled by the shell | shell navigates to `/login?reason=expired&return_to=<path+query>` |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

Anti-enumeration rules decided here (server side, requested): `invalid_credentials`
is the same for unknown email and wrong password; `account_locked` and
`rate_limited` count attempts per email and per IP whether or not the email
exists; `email_taken` is only returned by register (accepted trade-off, the
copy stays neutral). The UI never reveals which of email or password was wrong.

Social identity rules decided here (server side, requested): the identity key is
`(provider, subject)` and never the email; a provider email that matches an
existing account returns `email_taken` and never signs in or links; an
unverified or missing provider email creates nothing; an identity already
linked to another account returns `identity_taken`; link mode requires a
session and is only reachable from Profile. The UI never branches on a provider
id beyond looking up its configured name and mark.

Contract requests are only requested here; OpenAPI is changed by the build
tickets. Unsafe calls need `X-CSRF-Token` equal to `xf_csrf`; the first `GET
/auth/config` guarantees the cookie exists.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Sign in | submit, Enter in any field | Validate; `POST /auth/login`; on 200 clear the password from memory and go to `return_to` (validated), else `/`, `/admin` for admins; apply `User.locale` to `<html lang>` |
| Toggle password visibility | eye button | Switches `type` password/text; button `aria-pressed`; resets to hidden after submit |
| Sign in with passkey | button (only if `passkey_enabled` and `PublicKeyCredential` exists) | Options, `navigator.credentials.get`, verify; user cancel shows nothing; failure shows `auth.error.passkey` |
| Continue with provider | one button per enabled provider | Full-page navigation to `GET /auth/social/{provider}/start` with the validated `return_to` and the current origin; the button shows a spinner and all auth controls become read-only; browser Back only returns to this page |
| Retry a social provider | `Try again` in the social error banner | Same navigation as above with the same provider, `return_to` and origin |
| Sign in instead | `auth.social.email-taken-cta` | Focus moves to the email field, the social banner clears, and the normal sign-in form is used; after sign-in the person links the provider in Profile |
| Use another method | `auth.social.register.back` | Discards the ticket and goes to `/login` without a banner |
| Complete social sign-up | submit on `/register/social` | Validate name and terms; `POST /auth/social/complete`; on 201 apply `User.locale`, clear the ticket from the URL and go to `/onboarding` |
| Go to register / login | footer link | Client route, keeps `return_to`, clears banners and errors |
| Create account | submit | Validate; `POST /auth/register`; on 201 `POST /auth/login` with the same values, then `/onboarding`. If that login fails, go to `/login` with toast `auth.register.created-signin` |
| Retry (config or network error) | button | Repeat `GET /auth/config` or the failed call, focus returns to the form |
| Switch language / theme | header toggles | Immediate, stored locally |
| Dismiss banner | close button | Only banners with a close button (expired). Error banners clear on the next input |

## Interactions

- Validation runs on submit and on blur after a field was touched; after the
  first failed submit it re-runs on input. Submit is never disabled for
  validation reasons (only while sending or blocked), so the reason is visible.
- Client checks: email non-empty and contains `@` with text on both sides, max
  254; password non-empty (login), 8 to 128 (register); confirm equals
  password; terms checked. The server remains authoritative.
- On a failed submit focus moves to the first invalid field; on a server error
  focus moves to the banner (`tabindex=-1`), then Tab continues to the form.
- Login failure clears the password field and keeps the email.
- Double submit is ignored while a request is in flight.
- A social button is not a fetch. It saves nothing in `localStorage` and leaves
  the page, so the browser Back button returns to a fresh auth page. After the
  callback the page reads `?social` once, shows the banner and removes the
  query parameters.
- The provider group renders from `GET /auth/config`; adding or removing a
  provider changes the number of buttons without a code change. The copy and
  test ids use the configured provider id, so a fake provider works exactly
  like a real one.
- `/register/social` reads the ticket from the URL, never from storage. It
  exposes no password field and the email field is read-only because the
  provider already verified it. Submit is blocked while sending; an expired
  ticket replaces the card with the expired state and `Use another method`.
- Rate limit and lock use a local countdown from `retry_after_seconds` or
  `locked_until`; when it reaches 0 the banner is replaced by an info line and
  the button is enabled again. The countdown is read by screen readers only at
  the start and every 60 s (see Accessibility).
- Another tab that signs in sets `localStorage` `xf.signed-in`; an open
  `/login` tab reacts to the `storage` event by redirecting as above.
- Unsaved work from an expired session is restored by the shell after sign-in
  (see user-shell); this page only preserves `return_to`.
- Keyboard: Enter submits, Esc does nothing, no shortcuts (global shortcuts
  are off on these pages).

## States

State scope: the card content. The page frame (header, hero) is always shown.

### Ready

Login form as in Layout with empty fields, focus on Email. `?reason=expired`
adds the info banner `auth.expired.title` + `auth.expired.text` above the
fields (focus still on Email; the banner is `role=status`). Register shows the
rules list in its neutral state and an unchecked terms box.

The social group shows one button per enabled provider in server order. With
four real providers the login and register cards show Google, Facebook, X and
Apple. With no provider the divider and the group are absent and the passkey
button follows the submit button directly.

`/register/social` with a valid ticket is the first-time Ready state: the
completion card from Layout with the provider name, verified email, suggested
display name and an unchecked terms box, focus on the display name. It has no
password fields and no provider group.

### Loading

- Page boot (`GET /me` + `GET /auth/config`): show the card skeleton only after
  150 ms (title bar, two field bars, button bar), `aria-busy` on the card, no
  text. Timeout 8 s then Error.
- Submitting: fields read-only (not disabled, so values stay readable), the
  submit button shows a spinner and the label `auth.login.submitting` /
  `auth.register.submitting`, passkey and links are disabled. Slow call over 10
  s keeps the spinner and adds `auth.loading.slow` under the button.
- Social redirect: the clicked provider button shows a spinner and the label
  `auth.social.redirecting`; every social button, the submit button, the passkey
  button and the footer links are disabled until the browser leaves the page.
  The button is not a request with an error state of its own; a blocked
  navigation returns to Ready and shows `auth.social.failed`.
- Social completion submit: the card fields are read-only, the submit button
  shows a spinner and `auth.social.register.submitting`, and the back link is
  disabled. Timeout after 10 s becomes the card error with the values kept.

### Empty

There is no data list. Empty means a closed door: `/register` while
`registration_open = false` (or after a 409 `registration_closed`) replaces
the form with title `auth.register.closed.title`, text
`auth.register.closed.text` and the call to action `auth.register.closed.cta`
("Back to sign in"). `/login` simply hides the register link.

An empty `providers[]` is not an error: the social group and its divider are
absent and the rest of the card is unchanged. `/register/social` with a
missing or unknown ticket shows the same closed-door treatment with title
`auth.social.register.expired.title`, text
`auth.social.register.expired.text` and the call to action
`auth.social.register.back`.

### Error

- Invalid credentials (401): banner (bad) `auth.error.invalid-credentials`,
  both fields `aria-invalid`, password cleared.
- Field validation: message under the field (text + icon), listed in Copy keys;
  server `details.fields` map to the same keys.
- Email already used (409 `email_taken`): field error `auth.error.email-taken`
  with link "Sign in instead" to `/login` (email carried over).
- Rate limited (429): warning banner `auth.error.rate-limited` with the minutes
  left, button disabled until the countdown ends.
- Locked account (423): warning banner `auth.error.locked` with the unlock time,
  button disabled; link to `/login` stays. No unlock action exists for users.
- Disabled account (403 `account_disabled`): banner (bad) `auth.error.disabled`.
- Network or 5xx (also config failure that is not 404): banner (bad)
  `auth.error.server` with button `common.retry` (`auth-error-retry`). Form
  values are kept. The same banner copy is used for timeouts.
- Passkey failure: banner `auth.error.passkey` and the password form stays.

### Social errors

Every social failure returns to the login or register page that started the
flow with `?social=<code>&provider=<id>`. The banner is placed above the fields,
takes focus (`tabindex=-1`) on arrival, and is cleared when the person starts a
new sign-in or chooses another method. The provider name in every message comes
from `providers[]`; no provider-specific copy exists.

- Email already registered (`email_taken`): bad banner
  `auth.social.email-taken` with `auth.social.email-taken-cta` ("Sign in
  instead"). The message says the account exists and that the person must sign
  in first and then link the provider in Profile. Choosing the CTA keeps the
  email if the provider shared it, focuses the email field and clears the
  banner. No session is created and no link is made.
- Provider denied or cancelled (`denied`): info banner
  `auth.social.denied`, plus `Try again` which repeats the same provider. This
  is a normal outcome, not an error tone.
- Unverified email from the provider (`email_unverified`): bad banner
  `auth.social.email-unverified` and `auth.social.email-use`. No account is
  created or linked. The email and password form stays available.
- Identity already linked to another account (`identity_taken`): bad banner
  `auth.social.identity-taken` and `auth.social.email-use`. Nothing is moved.
- Provider unavailable or misconfigured (`unavailable`): bad banner
  `auth.social.unavailable` and `auth.social.email-use`.
- Expired or invalid state, nonce or PKCE (`state_invalid`): bad banner
  `auth.social.state-invalid` and `Try again`. The server issues a fresh state
  on the next start.
- Network or provider failure (`failed`): bad banner `auth.social.failed` with
  `Try again`; the email form is unchanged.
- Disabled account (`account_disabled`): bad banner `auth.error.disabled`; no
  social retry is offered.
- Social sign-up failures on `/register/social`: 400 `validation_failed` maps to
  the same field errors as register; 409 `email_taken` shows
  `auth.social.email-taken` with `auth.social.email-taken-cta`; 409
  `identity_taken` shows `auth.social.identity-taken`; 410 `ticket_expired`
  replaces the card with the empty expired state.

### Partial and stale

If only `GET /auth/config` fails but `GET /me` answered 401, the form is shown
with safe defaults (registration link shown, passkey hidden, social group
hidden, no terms links) and a small muted line `auth.config.partial`;
submitting still works. Config is cached in memory for the page lifetime only,
never in `localStorage`. A social button is never shown from a cached provider
list across page loads; a stale list would send the person to a provider the
server no longer serves.

### Forbidden

- 403 `csrf_rejected`: refresh the token with `GET /auth/config`, resend once
  automatically; a second 403 shows the Error banner `auth.error.server`.
- A signed-in user opening these routes is redirected, never shown a 403.
- 403 `account_disabled` is shown as above.

### Session expired

`/login?reason=expired&return_to=/albums/42?sort=new`: shown when the shell
detected a 401. Differences from normal sign in: info banner, `h1` is
`auth.expired.heading`, the submit label is `auth.expired.submit` ("Sign in and
continue"), the register link is replaced by "Use a different account" which
clears `reason`/`return_to` after a confirm-free navigation to `/login`. After
a successful sign in the user lands on `return_to` (replace, so Back does not
return to the login page). If `return_to` is missing or invalid the user lands
on `/` and the banner still shows once. A different user signing in than the
expired session is allowed; the shell clears cached data for the old user.

## Live updates

Not live. There is no SSE and no polling here; the shell stops both on a 401
before sending the user to this page. The only timers are the local
rate-limit/lock countdown and the 8 s boot timeout. Cross-tab sign-in uses the
`storage` event described in Interactions.

## Responsive

- >= 900 px: two columns (hero + card); card is vertically centred.
- 600 to 899 px: single column, card centred, hero hidden.
- < 600 px (390 px reference): card has no border or shadow, fills width with
  16 px padding; the header stays one row (brand + EN + theme icon). Buttons are
  full width, 44 px high. Inputs use 16 px text so iOS does not zoom.
- The on-screen keyboard must not hide the submit button: the card scrolls and
  the focused field is scrolled into view. Reflows at 320 px without loss.

## Accessibility

- One `h1` per screen (`auth.login.title`, `auth.register.title`,
  `auth.expired.heading`, `auth.social.register.title`). Form is a native
  `<form>` with `novalidate`; every field has a visible `<label for>`; required
  fields say "required" in text.
- The social provider group is a labelled `group`; every button's accessible
  name is `auth.social.continue` with the provider name, so the mark is
  `aria-hidden` and never carries meaning alone. The group sits after the
  divider in focus order and before the passkey button.
- Social result banners use `role=alert` for bad and `role=status` for the
  cancelled (info) case. The banner receives focus when it arrives from a
  callback, and `Try again` is the next tab stop. The completion card labels
  the read-only provider email as such, not only by its value.
- Errors: field message linked with `aria-describedby`, `aria-invalid`, text
  plus icon. The banner for failures is `role=alert`; the expired banner and
  the countdown are `role=status` (`aria-live=polite`). Countdown text is
  announced at the start and each 60 s, not each second.
- Focus order: header controls, form, submit, passkey, footer link. No focus
  trap. A visible focus ring (accent halo) on every control; targets >= 44 px
  on mobile, >= 40 px on desktop.
- Password toggle: `aria-label` `auth.password.show` / `auth.password.hide`
  plus `aria-pressed`. Password rules list is a `ul` under the field; its
  met/unmet state is text ("met"/"not met" visually hidden) not colour only.
- Show-password and paste must not be blocked. `autocomplete` values as in
  Layout so browsers and managers fill reliably.
- Contrast >= 4.5:1 in light and dark; reduced motion stops the skeleton
  shimmer and the spinner rotation (static ring).
- `<html lang>` follows the language toggle and updates on switch.

## i18n

Keys follow `auth.<area>.<element>` (i18n.md). Placeholders are named. EN is the
source; VI must exist for every key. Text expands 30 %: buttons wrap to two
lines on 390 px rather than truncate. Dates and countdown use the active locale.

| Key | EN | VI |
| --- | --- | --- |
| `auth.hero.title` | Your Flickr circle, kept and searchable | Vòng kết nối Flickr của bạn, được lưu giữ và dễ tìm |
| `auth.header.language` | Switch to Tiếng Việt | Chuyển sang English |
| `auth.header.theme` | Switch between light and dark | Chuyển giữa sáng và tối |
| `auth.login.title` | Sign in | Đăng nhập |
| `auth.login.intro` | Welcome back to XFlickr. | Chào mừng bạn quay lại XFlickr. |
| `auth.login.submit` | Sign in | Đăng nhập |
| `auth.login.submitting` | Signing in… | Đang đăng nhập… |
| `auth.login.passkey` | Sign in with a passkey | Đăng nhập bằng passkey |
| `auth.login.or` | or | hoặc |
| `auth.login.to-register` | No account? Create one | Chưa có tài khoản? Tạo tài khoản |
| `auth.field.email` | Email | Email |
| `auth.field.email-help` | We use it only to sign you in. | Chúng tôi chỉ dùng để bạn đăng nhập. |
| `auth.field.password` | Password | Mật khẩu |
| `auth.field.confirm` | Confirm password | Nhập lại mật khẩu |
| `auth.field.required` | required | bắt buộc |
| `auth.password.show` | Show password | Hiện mật khẩu |
| `auth.password.hide` | Hide password | Ẩn mật khẩu |
| `auth.password.rules-title` | Your password needs | Mật khẩu cần có |
| `auth.password.rule-length` | {min} to {max} characters | Từ {min} đến {max} ký tự |
| `auth.password.rule-email` | Different from your email | Khác với email của bạn |
| `auth.password.rule-met` | met | đạt |
| `auth.password.rule-unmet` | not met | chưa đạt |
| `auth.register.title` | Create your account | Tạo tài khoản của bạn |
| `auth.register.intro` | Start keeping your Flickr circle in one place. | Bắt đầu lưu vòng kết nối Flickr của bạn ở một nơi. |
| `auth.register.submit` | Create account | Tạo tài khoản |
| `auth.register.submitting` | Creating account… | Đang tạo tài khoản… |
| `auth.register.terms` | I agree to the {terms} and the {privacy} | Tôi đồng ý với {terms} và {privacy} |
| `auth.register.terms-link` | Terms of use | Điều khoản sử dụng |
| `auth.register.privacy-link` | Privacy notice | Thông báo quyền riêng tư |
| `auth.register.to-login` | Already registered? Sign in | Đã có tài khoản? Đăng nhập |
| `auth.register.created-signin` | Account created. Please sign in. | Đã tạo tài khoản. Hãy đăng nhập. |
| `auth.register.closed.title` | Registration is closed | Đăng ký đang đóng |
| `auth.register.closed.text` | New accounts are not being accepted right now. Ask the administrator if you need access. | Hiện chưa nhận tài khoản mới. Hãy hỏi quản trị viên nếu bạn cần quyền truy cập. |
| `auth.register.closed.cta` | Back to sign in | Quay lại đăng nhập |
| `auth.expired.heading` | Your session ended | Phiên của bạn đã kết thúc |
| `auth.expired.title` | You were signed out | Bạn đã bị đăng xuất |
| `auth.expired.text` | For your safety your session expired. Sign in to continue where you left off. | Vì an toàn, phiên của bạn đã hết hạn. Hãy đăng nhập để tiếp tục ở nơi bạn đã dừng. |
| `auth.expired.submit` | Sign in and continue | Đăng nhập và tiếp tục |
| `auth.expired.other-account` | Use a different account | Dùng tài khoản khác |
| `auth.validation.email-required` | Enter your email. | Hãy nhập email. |
| `auth.validation.email-invalid` | Enter a valid email, like `name@example.com`. | Hãy nhập email hợp lệ, ví dụ `ten@example.com`. |
| `auth.validation.password-required` | Enter your password. | Hãy nhập mật khẩu. |
| `auth.validation.password-short` | Use at least {min} characters. | Hãy dùng ít nhất {min} ký tự. |
| `auth.validation.password-long` | Use at most {max} characters. | Hãy dùng tối đa {max} ký tự. |
| `auth.validation.confirm-mismatch` | The passwords do not match. | Hai mật khẩu không khớp. |
| `auth.validation.terms-required` | Accept the terms to create an account. | Hãy đồng ý điều khoản để tạo tài khoản. |
| `auth.error.invalid-credentials` | The email or password is not right. Check them and try again. | Email hoặc mật khẩu chưa đúng. Hãy kiểm tra và thử lại. |
| `auth.error.email-taken` | An account with this email already exists. | Đã có tài khoản dùng email này. |
| `auth.error.email-taken-cta` | Sign in instead | Đăng nhập thay thế |
| `auth.error.rate-limited` | Too many attempts. Try again in {minutes} min. | Quá nhiều lần thử. Hãy thử lại sau {minutes} phút. |
| `auth.error.locked` | This account is locked after repeated failed sign-ins. It unlocks at {time}. | Tài khoản bị khóa sau nhiều lần đăng nhập sai. Sẽ mở khóa lúc {time}. |
| `auth.error.disabled` | This account is disabled. Contact the administrator. | Tài khoản này đã bị vô hiệu hóa. Hãy liên hệ quản trị viên. |
| `auth.error.server` | We could not reach XFlickr. Your input is kept. Check your connection and try again. | Không kết nối được XFlickr. Nội dung bạn nhập vẫn được giữ. Hãy kiểm tra mạng và thử lại. |
| `auth.error.passkey` | The passkey did not work. Use your password instead. | Passkey không dùng được. Hãy dùng mật khẩu. |
| `auth.social.group` | Continue with another account | Tiếp tục bằng tài khoản khác |
| `auth.social.continue` | Continue with {provider} | Tiếp tục với {provider} |
| `auth.social.redirecting` | Redirecting to {provider}… | Đang chuyển đến {provider}… |
| `auth.social.email-taken` | An XFlickr account already uses this email. Sign in with your password, then link {provider} in Profile. | Đã có tài khoản XFlickr dùng email này. Hãy đăng nhập bằng mật khẩu rồi liên kết {provider} trong Hồ sơ. |
| `auth.social.email-taken-cta` | Sign in instead | Đăng nhập thay thế |
| `auth.social.denied` | {provider} sign-in was cancelled. Try again or use your email. | Đăng nhập bằng {provider} đã bị hủy. Hãy thử lại hoặc dùng email. |
| `auth.social.email-unverified` | {provider} did not confirm your email address. Use another sign-in method. | {provider} chưa xác nhận địa chỉ email của bạn. Hãy dùng cách đăng nhập khác. |
| `auth.social.email-use` | Use email instead | Dùng email thay thế |
| `auth.social.identity-taken` | This {provider} account is already linked to another XFlickr account. | Tài khoản {provider} này đã liên kết với một tài khoản XFlickr khác. |
| `auth.social.unavailable` | {provider} sign-in is not available right now. Try again later or use your email. | Đăng nhập bằng {provider} hiện không khả dụng. Hãy thử lại sau hoặc dùng email. |
| `auth.social.state-invalid` | This sign-in attempt expired. Start again. | Lần đăng nhập này đã hết hạn. Hãy bắt đầu lại. |
| `auth.social.failed` | We could not finish signing in with {provider}. Try again or use your email. | Không hoàn tất đăng nhập bằng {provider}. Hãy thử lại hoặc dùng email. |
| `auth.social.retry` | Try {provider} again | Thử lại với {provider} |
| `auth.social.register.title` | Finish creating your account | Hoàn tất tạo tài khoản |
| `auth.social.register.text` | You signed in with {provider}. Confirm your details to create your XFlickr account. | Bạn đã đăng nhập bằng {provider}. Hãy xác nhận thông tin để tạo tài khoản XFlickr. |
| `auth.social.register.email` | Email from {provider} | Email từ {provider} |
| `auth.social.register.name` | Display name | Tên hiển thị |
| `auth.social.register.submit` | Create account | Tạo tài khoản |
| `auth.social.register.submitting` | Creating account… | Đang tạo tài khoản… |
| `auth.social.register.back` | Use another method | Dùng cách khác |
| `auth.social.register.expired.title` | This sign-up attempt expired | Lần đăng ký này đã hết hạn |
| `auth.social.register.expired.text` | Start again from the sign-in or register page. | Hãy bắt đầu lại từ trang đăng nhập hoặc đăng ký. |
| `auth.config.partial` | Some options could not load. You can still sign in. | Một số tùy chọn chưa tải được. Bạn vẫn có thể đăng nhập. |
| `auth.loading.slow` | Still working… | Vẫn đang xử lý… |
| `common.retry` | Try again | Thử lại |
| `common.close` | Close | Đóng |

## Test IDs

Names follow patterns.md (`auth-<area>-<element>`). Repeated items use a stable
suffix. Tests select by role first and by test id when text depends on language.

- Page: `auth-page`, `auth-header`, `auth-brand`, `auth-lang-toggle`,
  `auth-theme-toggle`, `auth-hero`, `auth-card`, `auth-skeleton`.
- Login: `auth-login-form`, `auth-login-title`, `auth-login-email`,
  `auth-login-password`, `auth-login-password-toggle`, `auth-login-submit`,
  `auth-login-passkey`, `auth-login-to-register`.
- Register: `auth-register-form`, `auth-register-title`,
  `auth-register-email`, `auth-register-password`,
  `auth-register-password-toggle`, `auth-register-confirm`,
  `auth-register-rules`, `auth-register-rule-length`,
  `auth-register-rule-email`, `auth-register-terms`,
  `auth-register-terms-link`, `auth-register-privacy-link`,
  `auth-register-submit`, `auth-register-to-login`.
- Field errors: `auth-error-email`, `auth-error-password`,
  `auth-error-confirm`, `auth-error-terms`, `auth-error-email-taken-link`.
- Banners: `auth-expired-banner`, `auth-expired-dismiss`,
  `auth-expired-other-account`, `auth-error-banner`, `auth-error-retry`,
  `auth-ratelimit-banner`, `auth-locked-banner`, `auth-disabled-banner`,
  `auth-partial-note`.
- Social: `auth-social-group` and one `auth-social-<provider>` per enabled
  provider (`google`, `facebook`, `x`, `apple`; `fake` in tests). Error banners
  are `auth-social-email-taken-banner`, `auth-social-denied-banner`,
  `auth-social-unverified-banner`, `auth-social-identity-banner`,
  `auth-social-unavailable-banner`, `auth-social-state-banner`,
  `auth-social-failed-banner`; actions are `auth-social-email-taken-cta`,
  `auth-social-retry`, `auth-social-use-email`.
- Social completion: `auth-social-register-card`, `auth-social-register-title`,
  `auth-social-register-email`, `auth-social-register-name`,
  `auth-social-register-terms`, `auth-social-register-submit`,
  `auth-social-register-back`, `auth-social-register-expired`.
- Empty: `auth-register-closed`, `auth-register-closed-cta`.

## Test hooks

The `data-testid` list above is the contract for `web/e2e/user-auth.spec.ts`.
Minimum journeys: sign in and land on `/`; wrong password; rate limit and lock
(mock 429/423); register then onboarding; closed registration; passkey button
hidden when the flag is off; expired session returns to `return_to`; hostile
`return_to` (`//evil.example`) falls back to `/`; EN/VI switch; 390 px layout.
Social journeys use the fake provider: one button per configured provider and
none when the list is empty; sign in with the fake provider; first-time
completion then onboarding; `email_taken` then `Sign in instead`; `denied`,
`email_unverified`, `identity_taken`, `state_invalid`, `unavailable` and
`failed` banners; expired sign-up ticket; EN/VI copy and 390 px layout.

## Out of scope

- Password reset, email verification, "remember me", MFA beyond passkey, and
  account deletion (none exist in this release).
- Registering real provider applications, provider brand assets and provider
  consent-screen content (release 1 uses the fake provider for tests).
- Passkey enrolment and account linking (Profile concerns) and the Setup
  wizard.
- The in-app session-expired modal and global shell states (user-shell).
- The ADMIN sign-in look (admin uses the same page; role is only known after
  login), OpenAPI edits (only requested above) and application code.
