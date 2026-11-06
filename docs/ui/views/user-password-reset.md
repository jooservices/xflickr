# Password reset

View id `user-password-reset`, role USER (public), light gallery theme (dark
optional). Two public screens share the auth page frame from
[user-auth](user-auth.md): request a link `/forgot-password` and choose a new
password `/reset-password?token=...`. Shared rules:
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-password-reset.html](../mockups/user-password-reset.html). Build
ticket: TBD (parent JOOXF-279).

## Purpose

Let a person who forgot the password ask for a reset link by email and choose a
new password from that link. The request screen never reveals whether an email
has an account. The reset screen validates the link before showing the form,
applies the same password rules as registration, and signs the account out
everywhere when the password changes.

## Route and access

- `/forgot-password`: public. A signed-in user is redirected to
  `/profile#security` with the toast `auth.reset.signed-in-redirect` because the Profile
  password form is the normal path; the reset link opened from email still
  works in any browser.
- `/reset-password?token=<token>`: public. Without a token the Empty state is
  shown. A signed-in user may open the link; on success every session for the
  account is revoked, including the current one, and the person signs in again.
- Both routes accept no `return_to`; after a successful reset the person goes to
  `/login?reason=reset`. The sign-in screen then keeps its normal return
  behaviour.
- A signed-in administrator is not redirected by this public route; the page
  behaves like the signed-out flow and the reset still revokes the session.
- Theme and language behave as in user-auth. Document titles:
  `Reset your password · XFlickr` and `Choose a new password · XFlickr`.

## Layout

The auth frame is reused: header with brand and toggles, optional decorative
hero, one centred card. The card is 420 px on desktop and full width minus
32 px on mobile.

- Request card: `h1` `auth.reset.request.title`, intro
  `auth.reset.request.intro`, email field (`type=email`,
  `autocomplete=email`), primary button `auth.reset.request.submit`, and the
  footer link `auth.reset.to-signin`.
- Sent card: mail icon, `h1` `auth.reset.sent.title`, text
  `auth.reset.sent.text` with the email, expiry note `auth.reset.sent.expires`
  with the configured minutes, a secondary `auth.reset.sent.again` button
  disabled during the cooldown, and the link `auth.reset.sent.signin`.
- Reset card: `h1` `auth.reset.confirm.title`, intro
  `auth.reset.confirm.intro` with the masked email, new password and confirm
  fields with show/hide toggles, the same live rules list as registration, and
  the primary button `auth.reset.confirm.submit`. Paste and password managers
  are allowed.
- Changed card: check icon, `h1` `auth.reset.changed.title`, text
  `auth.reset.changed.text`, primary action `auth.reset.changed.signin`.
- Empty card: link icon, `h1` `auth.reset.empty.title`, text
  `auth.reset.empty.text`, primary action `auth.reset.empty.cta`.
- Error card: bad `banner` above the form or body and a retry action when the
  failure is retryable.
- Components used: `card`, `button`, `text-field`, `banner`, `empty-block`,
  `skeleton`. No new component is introduced.

## Data

Short form: the request screen posts one email; the reset screen validates one
token and posts the new password. Fields and endpoints are in
[Data and API](#data-and-api).

## Data and API

The route names in this section are provisional contract requests. Final names
are fixed by the strict REST rules of JOOXF-314 and JOOXF-316 (resources as
nouns, the HTTP method as the verb). The behaviour, payloads, error cases and
states documented below are binding regardless of the final names.

| Element | Source | Fields used |
| --- | --- | --- |
| Page config | `GET /auth/config` (contract request from user-auth) | add `reset_ttl_minutes` (int), `reset_resend_seconds` (int); `password_min` (8) and `password_max` (128) already exist |
| Request link | `POST /auth/password-reset/request` **contract request** body `{email}` | 202 always; 400 `validation_failed`; 429 `rate_limited` with `Retry-After` and `details.retry_after_seconds` |
| Validate link | `GET /auth/password-reset/token?token=<token>` **contract request** | 200 `{email_masked, expires_at}`; 410 `reset_token_invalid` or `reset_token_expired`; 429 `rate_limited` |
| Choose password | `POST /auth/password-reset/confirm` **contract request** body `{token, new_password}` | 204 and the server revokes every session; 400 `validation_failed` with `details.fields`; 410 `reset_token_invalid` or `reset_token_expired`; 403 `account_disabled`; 429 `rate_limited` |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

The server sends the email through the email port; development and tests use
the Mailpit sink. Unsafe calls carry `X-CSRF-Token` equal to `xf_csrf`.
Contract requests are only requested here; OpenAPI changes belong to the build
tickets.

Anti-enumeration: `POST /auth/password-reset/request` answers 202 for an
unknown, verified, unverified, disabled or rate-limited address, except that a
rate-limited client receives 429 so it can stop. The success copy is identical
in every 202 case. `GET /auth/password-reset/token` returns the same 410 body
for an unknown, used or expired token and for a disabled account.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Request a link | submit or Enter in the email field | Validate; `POST /auth/password-reset/request`; on 202 replace the form with the neutral sent card |
| Ask again | `auth.reset.sent.again` | Repeat the request with the stored email after the cooldown; keep the sent card |
| Validate link | page mount with a token | `GET /auth/password-reset/token`; on 200 show the form, on 410 show Empty/Error, on network failure show Error with Retry |
| Choose a new password | submit | Validate; `POST /auth/password-reset/confirm`; on 204 show the changed card |
| Sign in | footer or success action | Client route `/login`; a `reason=reset` query shows the info banner `auth.reset.signed-in` |
| Back to request | empty or invalid-token action | Client route `/forgot-password`, carrying the email when known |
| Retry | error action | Repeat the failed request, show loading, move focus to the banner on a second failure |
| Toggle password visibility | eye button in each password field | Switches `type`; button `aria-pressed`; resets to hidden after submit |
| Switch language or theme | header toggles | Immediate, stored locally, `<html lang>` updated |

## Interactions

- The token is read from the query once and removed from the address bar with
  `history.replaceState` after validation starts. It is never written to
  storage, a log or an `aria-label`.
- Double submit is ignored while a request is in flight. A second visit to the
  same reset link after success shows the Empty state with
  `auth.reset.empty.used` because the token is single-use.
- Validation runs on submit and on blur after a field was touched; after the
  first failed submit it re-runs on input. Submit is never disabled for
  validation reasons, only while sending or rate limited.
- Client checks: email non-empty, contains `@` with text on both sides, max
  254; password 8 to 128 characters and not equal to the email on the reset
  screen; confirm equals the new password. The server remains authoritative.
- On a failed submit focus moves to the first invalid field. On a server error
  focus moves to the banner (`tabindex=-1`), then Tab continues to the form.
- The reset password fields keep their values on a server failure. The request
  email is kept and prefilled when the person goes back from an invalid link.
- The resend button is disabled during the cooldown and shows the countdown in
  the active locale. The countdown is announced at the start and every 60 s.
- Keyboard: Enter submits from a field. Esc does nothing. No view shortcuts;
  global shortcuts are off on auth pages.
- Focus order: header controls, form, primary action, footer link. On the
  changed card, focus moves to the `h1`.

## States

State scope: the auth card. The header and optional hero are always shown.

### Ready

- Request: empty email field, focus on Email, submit enabled.
- Sent: neutral card with the email, expiry note and a disabled resend button
  until the cooldown ends.
- Reset: masked email in the intro, empty password fields, rules list in its
  neutral state, submit enabled.
- Changed: check icon, title, text, sign-in action.

### Loading

- Page boot (`GET /auth/config` and, on the reset route, the token validation):
  the card skeleton appears after 200 ms (title bar, two field bars, button
  bar), `aria-busy` on the card, no text. Timeout 10 s becomes Error.
- Requesting or resending: button spinner and label `auth.reset.requesting`;
  the email field is read-only; the footer link is disabled.
- Validating the link: spinner and `auth.reset.validating` in a status line.
- Choosing a password: the password fields are read-only, the submit button
  shows `auth.reset.confirm.submitting`, show/hide buttons are disabled.
- A slow call over 10 s keeps the spinner and adds `auth.reset.loading.slow`.

### Empty

- `/reset-password` without a token: link icon, title `auth.reset.empty.title`,
  text `auth.reset.empty.text`, action `auth.reset.empty.cta` (Request a new
  link).
- A used token (410 `reset_token_invalid` after a previous success): title
  `auth.reset.empty.used-title`, text `auth.reset.empty.used`, same action.
- The request screen has no empty list; its empty fields are Ready.
- Empty is not an error: no request failed and no account is revealed.

### Error

- Invalid or expired token (410): bad banner `auth.reset.error.token` and the
  action `auth.reset.empty.cta`. The copy does not say whether the account
  exists.
- Request network or 5xx, also a config failure that is not 404: bad banner
  `auth.reset.error.server` with `common.retry` (`auth-reset-error-retry`).
  The email is kept. The same copy is used for timeouts.
- Rate limited (429): warning banner `auth.reset.error.rate-limited` with the
  minutes left; the button is disabled until the countdown ends.
- Field validation: message under the field (text plus icon), linked with
  `aria-describedby`. Server `details.fields` map to the same keys. The server
  may return `invalid`, `too_short`, `too_long` or `required`.
- A failed password change does not clear the password fields.
- Field errors are never shown as a toast.

### Forbidden

- `403 account_disabled`: shown with the generic invalid-token copy, never with
  a different message, so a disabled account is not confirmed.
- `403 csrf_rejected`: refresh the token with `GET /auth/config` and resend
  once automatically. A second 403 becomes the server error banner.
- A signed-in user opening `/forgot-password` is redirected to Profile, not
  shown a 403. A signed-in user opening a reset link is allowed; success signs
  the account out everywhere.
- A valid token used by a different browser is allowed: the token is the
  authority, and the copy stays neutral.

### Success

- Request accepted: neutral sent card with title `auth.reset.sent.title`, text
  `auth.reset.sent.text`, expiry `auth.reset.sent.expires`, resend cooldown and
  sign-in link. Announce once through `role=status`.
- Password changed: check icon, title `auth.reset.changed.title`, text
  `auth.reset.changed.text`, sign-in action. Announce once through
  `role=status`.
- No automatic redirect before the person reads the copy; the sign-in action is
  the only navigation.

### Partial and stale

If only `GET /auth/config` fails but the reset token validates, the form is
shown with safe defaults (8 to 128 characters, 60-minute link life, 60-second
resend cooldown) and a muted line `auth.reset.config.partial`. The config is
cached in memory for the page lifetime only, never in `localStorage`.

### Session expired

A signed-in user who opens `/forgot-password` is redirected to Profile, so the
expired-session modal belongs to the shell. On the public reset route there is
no session check; a 401 from the changed-password call is not possible, and any
unexpected 401 shows the server error banner.

## Live updates

No SSE and no polling. The only timers are the resend cooldown, the request
countdown and the 10 s boot timeout. A successful reset on another device
revokes this session; the next request answers 401 and the shell handles it.

## Responsive

- >= 900 px: two columns (hero and card), card vertically centred.
- 600 to 899 px: one column, card centred, hero hidden.
- < 600 px (390 px reference): full-width card with 16 px padding, no border or
  shadow; header stays one row; buttons are full width and 44 px high; inputs
  use 16 px text so iOS does not zoom. The on-screen keyboard scrolls the
  focused field into view. Reflows at 320 px without loss.

## Accessibility

- One `h1` per screen. The request and reset cards are native `<form
  novalidate>` elements; every field has a visible `<label for>`; required
  fields say "required" in text.
- `autocomplete`: `email` on the request field, `new-password` on both reset
  fields. Paste and password managers are never blocked.
- Errors: field message linked with `aria-describedby`, `aria-invalid`, text
  plus icon. Failure banners use `role=alert`; the sent and changed cards and
  the cooldown use `role=status` (`aria-live=polite`).
- Focus order: header controls, form, submit, footer link. No focus trap. On a
  failed submit, focus moves to the first invalid field; on a server failure,
  to the banner heading; on a second retry failure, back to the banner.
- The password rules list is a `ul` under the field; its met/unmet state is text
  ("met"/"not met" visually hidden) not colour only.
- The show/hide control has an accessible name (`auth.reset.password.show` /
  `auth.reset.password.hide`) and `aria-pressed`.
- Targets >= 44 px on mobile and >= 40 px on desktop. Visible 2 px accent focus
  ring with 2 px offset. Contrast >= 4.5:1 in light and dark. Reduced motion
  stops the skeleton shimmer and spinner rotation (static ring, text remains).
- `<html lang>` follows the language toggle and updates on switch.

## i18n

Keys follow `auth.reset.<area>.<element>`. EN is the source; VI exists for every
key. Placeholders are named. Dates, times and counts use the active locale. Text
expands 30 % and buttons wrap on 390 px rather than truncate.

| Key | EN | VI |
| --- | --- | --- |
| `auth.reset.request.title` | Reset your password | Đặt lại mật khẩu |
| `auth.reset.request.intro` | Enter your email and we will send a link to choose a new password. | Nhập email của bạn và chúng tôi sẽ gửi liên kết để chọn mật khẩu mới. |
| `auth.reset.request.submit` | Send reset link | Gửi liên kết đặt lại |
| `auth.reset.requesting` | Sending… | Đang gửi… |
| `auth.reset.to-signin` | Back to sign in | Quay lại đăng nhập |
| `auth.reset.sent.title` | Check your email | Kiểm tra email của bạn |
| `auth.reset.sent.text` | If an account exists for {email}, a password reset link is on its way. | Nếu tài khoản tồn tại với {email}, liên kết đặt lại mật khẩu đang được gửi đến. |
| `auth.reset.sent.expires` | The link expires in {minutes} minutes. | Liên kết hết hạn sau {minutes} phút. |
| `auth.reset.sent.again` | Send again | Gửi lại |
| `auth.reset.sent.signin` | Back to sign in | Quay lại đăng nhập |
| `auth.reset.validating` | Checking your reset link… | Đang kiểm tra liên kết đặt lại… |
| `auth.reset.confirm.title` | Choose a new password | Chọn mật khẩu mới |
| `auth.reset.confirm.intro` | Set a new password for {email}. | Đặt mật khẩu mới cho {email}. |
| `auth.reset.confirm.new` | New password | Mật khẩu mới |
| `auth.reset.confirm.repeat` | Confirm new password | Nhập lại mật khẩu mới |
| `auth.reset.confirm.submit` | Change password | Đổi mật khẩu |
| `auth.reset.confirm.submitting` | Changing password… | Đang đổi mật khẩu… |
| `auth.reset.changed.title` | Password changed | Đã đổi mật khẩu |
| `auth.reset.changed.text` | All devices were signed out. Sign in with your new password. | Mọi thiết bị đã bị đăng xuất. Hãy đăng nhập bằng mật khẩu mới. |
| `auth.reset.changed.signin` | Sign in | Đăng nhập |
| `auth.reset.empty.title` | No reset link | Không có liên kết đặt lại |
| `auth.reset.empty.text` | Request a new link to choose a password. | Hãy yêu cầu liên kết mới để chọn mật khẩu. |
| `auth.reset.empty.used-title` | This reset link was already used | Liên kết đặt lại này đã được dùng |
| `auth.reset.empty.used` | Request a new link if you still need to change the password. | Hãy yêu cầu liên kết mới nếu bạn vẫn cần đổi mật khẩu. |
| `auth.reset.empty.cta` | Request a new link | Yêu cầu liên kết mới |
| `auth.reset.invalid.title` | This reset link is not valid | Liên kết đặt lại này không hợp lệ |
| `auth.reset.invalid.text` | It may have expired or already been used. Request a new link. | Liên kết có thể đã hết hạn hoặc đã được dùng. Hãy yêu cầu liên kết mới. |
| `auth.reset.error.token` | This reset link is not valid. Request a new one. | Liên kết đặt lại này không hợp lệ. Hãy yêu cầu liên kết mới. |
| `auth.reset.error.server` | We could not reach XFlickr. Your input is kept. Check your connection and try again. | Không kết nối được XFlickr. Nội dung bạn nhập vẫn được giữ. Hãy kiểm tra mạng và thử lại. |
| `auth.reset.error.rate-limited` | Too many requests. Try again in {minutes} min. | Quá nhiều yêu cầu. Hãy thử lại sau {minutes} phút. |
| `auth.reset.error.email-required` | Enter your email. | Hãy nhập email. |
| `auth.reset.error.email-invalid` | Enter a valid email, like `name@example.com`. | Hãy nhập email hợp lệ, ví dụ `ten@example.com`. |
| `auth.reset.error.password-required` | Enter a new password. | Hãy nhập mật khẩu mới. |
| `auth.reset.error.password-short` | Use at least {min} characters. | Hãy dùng ít nhất {min} ký tự. |
| `auth.reset.error.password-long` | Use at most {max} characters. | Hãy dùng tối đa {max} ký tự. |
| `auth.reset.error.password-email` | Choose a password different from your email. | Hãy chọn mật khẩu khác với email của bạn. |
| `auth.reset.error.confirm-mismatch` | The passwords do not match. | Hai mật khẩu không khớp. |
| `auth.reset.field.email` | Email | Email |
| `auth.reset.field.new` | New password | Mật khẩu mới |
| `auth.reset.field.repeat` | Confirm new password | Nhập lại mật khẩu mới |
| `auth.reset.field.required` | required | bắt buộc |
| `auth.reset.password.rules-title` | Your password needs | Mật khẩu cần có |
| `auth.reset.password.rule-length` | {min} to {max} characters | Từ {min} đến {max} ký tự |
| `auth.reset.password.rule-email` | Different from your email | Khác với email của bạn |
| `auth.reset.password.rule-met` | met | đạt |
| `auth.reset.password.rule-unmet` | not met | chưa đạt |
| `auth.reset.password.show` | Show password | Hiện mật khẩu |
| `auth.reset.password.hide` | Hide password | Ẩn mật khẩu |
| `auth.reset.signed-in` | Password changed. Sign in with the new password. | Đã đổi mật khẩu. Hãy đăng nhập bằng mật khẩu mới. |
| `auth.reset.signed-in-redirect` | Change your password in Profile. | Hãy đổi mật khẩu trong Hồ sơ. |
| `auth.reset.config.partial` | Some options could not load. You can still reset your password. | Một số tùy chọn chưa tải được. Bạn vẫn có thể đặt lại mật khẩu. |
| `auth.reset.loading.slow` | Still working… | Vẫn đang xử lý… |
| `common.retry` | Try again | Thử lại |
| `common.sign-in` | Sign in | Đăng nhập |

## Test IDs

Names follow patterns.md (`auth-reset-<element>` and
`auth-reset-confirm-<element>`); repeated items use a stable suffix.

- Request: `auth-reset-page`, `auth-reset-header`, `auth-reset-card`,
  `auth-reset-form`, `auth-reset-email`, `auth-reset-submit`,
  `auth-reset-to-signin`, `auth-reset-skeleton`.
- Sent: `auth-reset-sent`, `auth-reset-sent-again`, `auth-reset-sent-signin`,
  `auth-reset-cooldown`.
- Validate: `auth-reset-validating`, `auth-reset-empty`, `auth-reset-empty-cta`.
- Confirm: `auth-reset-confirm-form`, `auth-reset-confirm-title`,
  `auth-reset-confirm-email`, `auth-reset-confirm-new`,
  `auth-reset-confirm-repeat`, `auth-reset-confirm-show`,
  `auth-reset-confirm-rules`, `auth-reset-confirm-rule-length`,
  `auth-reset-confirm-rule-email`, `auth-reset-confirm-submit`.
- Changed: `auth-reset-changed`, `auth-reset-changed-signin`.
- Errors and notes: `auth-reset-error-email`, `auth-reset-error-password`,
  `auth-reset-error-repeat`, `auth-reset-error`, `auth-reset-error-retry`,
  `auth-reset-loading-slow`, `auth-reset-config-note`.

## Test hooks

The `data-testid` list above is the contract for
`web/e2e/user-password-reset.spec.ts`. Minimum journeys: request a link for a
known and an unknown email and see the same neutral sent card; open the link
from Mailpit and change the password; sign in with the new password and see the
old password rejected; expired and used links show the request action; password
rules and confirmation mismatch; resend cooldown; network failure keeps the
email and Retry succeeds; EN/VI switch; 390 px layout.

## Out of scope

- Changing the password while signed in (user-profile), resetting another
  user's password (admin-users), password history, and a real email provider.
- The sign-in screen itself (user-auth) and the shell session-expired modal
  (user-shell).
- OpenAPI edits and application code (only contract requests are recorded here).
