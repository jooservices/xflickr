# Email verification

View id `user-verify-email`, role USER (public until signed in), light gallery
theme (dark optional). Three public screens share the auth page frame from
[user-auth](user-auth.md): waiting for the email `/verify-email/pending`,
opening the link `/verify-email?token=...` and asking for a new link
`/verify-email/resend`. Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md). Mockup:
[user-verify-email.html](../mockups/user-verify-email.html). Build ticket: TBD
(parent JOOXF-279).

## Purpose

Let a new account prove that the email belongs to the person who registered,
before the account can use the product. The view also lets the person ask for a
new link, reports an expired or already-used link, and refuses a link that
belongs to another account. It owns no photo data and does not ask for a
password.

## Route and access

- `/verify-email/pending`: requires `xf_session`. 401 is handled by the shell
  (expired modal) and returns to `/login?reason=expired&return_to=/verify-email/pending`.
  When `GET /me` reports `email_verified = true`, redirect at once (replace) to
  `return_to` when it is a valid same-origin path, else `/`.
- `/verify-email?token=<token>`: public. The screen posts the token once on
  mount. A signed-in user may only use a token for their own account.
- `/verify-email/resend`: public. An optional `email` query prefills the field;
  a signed-in user resends for the session account without seeing the field.
- `return_to` follows the same validation as user-auth: a single leading `/`,
  no `//`, `\` or scheme, not `/login` or `/verify-email`, at most 2048
  characters. Invalid values are dropped silently and `/` is used.
- While `GET /me` reports `email_verified = false`, the shell keeps the user on
  the pending screen and shows no photo, key, crawl or profile route. The check
  is server side; the screen is the destination, not the security boundary.
- Theme: `data-role="user"`, stored choice or system preference. The EN/VI and
  theme toggles live in the auth header, as in user-auth. The locale is stored
  in `localStorage` `xf.locale`.
- Document titles: `Verify your email · XFlickr`,
  `Verifying your email · XFlickr`, `Send a verification link · XFlickr`,
  `Email verified · XFlickr`.

## Layout

The auth frame is reused unchanged: header with brand and toggles, optional
decorative hero, one centred card. The card is 420 px on desktop and full width
minus 32 px on mobile.

- Pending card: mail icon in a 72 px sunken circle, `h1` `auth.verify.pending.title`,
  the text `auth.verify.pending.text` with the masked email, the primary button
  `auth.verify.pending.resend`, a muted cooldown line while the button is
  disabled, and the link `auth.verify.pending.different`.
- Verifying card: the same card with `h1` `auth.verify.verifying.title`, a
  spinner and the status line `auth.verify.verifying.text`. No other control is
  shown; the request cannot be cancelled because it is idempotent and short.
- Resend card: `h1` `auth.verify.resend.title`, intro
  `auth.verify.resend.text`, an email field when signed out, the primary button
  `auth.verify.resend.submit`, and the link `auth.verify.back-to-signin`.
- Success card: check icon, `h1` `auth.verify.success.title`, text
  `auth.verify.success.text`, and one primary action. The action is
  `auth.verify.success.continue` when the link created a session, otherwise
  `auth.verify.success.signin`.
- Empty card: link icon, `h1` `auth.verify.empty.title`, text
  `auth.verify.empty.text`, primary action `auth.verify.empty.cta`.
- Forbidden card: lock icon, `h1` `auth.verify.forbidden.title`, text
  `auth.verify.forbidden.text`, primary action `auth.verify.forbidden.cta`.
- Error card: the same card with a bad `banner` above the form or body and a
  retry action when the failure is retryable.
- Components used: `card`, `button`, `text-field`, `banner`, `empty-block`,
  `skeleton`. No new component is introduced.

## Data

Short form: the pending screen reads the session and polls its verification
state; the link screen posts the token; the resend screen posts an email. Fields
and endpoints are in [Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Session and state | `GET /me` (exists) | add `email_verified` (bool), `email_verified_at` (ISO time, nullable), `onboarding_complete` (bool) |
| Page config | `GET /auth/config` (contract request from user-auth) | add `verification_required` (bool), `verification_ttl_hours` (int), `verification_resend_seconds` (int) |
| Confirm link | `POST /auth/verify-email` **contract request** body `{token}` | 200 `User` and `session_created` (bool); 400 `invalid_token`; 409 `already_verified`; 410 `token_expired`; 403 `wrong_user`; 429 `rate_limited` |
| Resend link | `POST /auth/verify-email/resend` **contract request** body `{email?}` | 202 always, whether or not the address has an account; 429 `rate_limited` with `Retry-After` and `details.retry_after_seconds` |
| Pending state | `GET /me` on window focus and every 10 s while the tab is visible | changes `email_verified` to true when the link was opened in another tab or device |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

The server sends the email through the email port; development and tests use
the Mailpit sink. The UI never calls an email provider. Unsafe calls carry
`X-CSRF-Token` equal to `xf_csrf`; `GET /auth/config` guarantees the cookie
exists. Contract requests are only requested here; OpenAPI changes belong to
the build tickets.

Anti-enumeration: the resend response is the same for an unknown address, an
already verified address and a known unverified address. The UI copy is the
same in all three cases.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Verify email | page mount with a token | `POST /auth/verify-email` once; on 200 show success or redirect; on 409 show the already-verified success copy |
| Ask for a new link (pending) | `auth.verify.pending.resend` | `POST /auth/verify-email/resend` with no body; start the cooldown; toast `auth.verify.resend.sent` |
| Ask for a new link (signed out) | resend form submit | Validate the email; `POST /auth/verify-email/resend` with `{email}`; replace the form with the neutral sent state |
| Open resend form | `auth.verify.empty.cta` or an expired-link error | Client route `/verify-email/resend`, carrying `email` when known |
| Continue | success primary action | Client route to the validated `return_to`, else `/onboarding` for a new account, else `/` |
| Sign in | success or forbidden action | Client route `/login`, carrying `return_to` when it is still valid |
| Use a different account | pending link | Clear the local session state and go to `/login?switch=1`; the shell clears cached data |
| Retry | error action | Repeat the failed request once, show loading, move focus to the banner on a second failure |
| Switch language or theme | header toggles | Immediate, stored locally, `<html lang>` updated |

## Interactions

- The link token is read from the query once and removed from the address bar
  with `history.replaceState` after the request starts, so a refresh does not
  send it again. The token is never written to storage or logs.
- Double submit is ignored while a request is in flight. A second mount with the
  same token is treated as `already_verified` and shows the success copy.
- A network failure keeps the token in memory and offers `Retry`; it does not
  send a new email. Closing the tab loses the token, which is acceptable
  because the email link remains.
- The resend button is disabled during the cooldown and shows
  `auth.verify.resend.cooldown` with the active locale's countdown. The
  countdown is announced at the start and every 60 s, never every second. At
  zero the line is replaced by `auth.verify.resend.ready`.
- The pending screen refetches `GET /me` on window focus and every 10 s while
  visible. When it becomes verified, the screen announces the success and
  redirects. It does not poll while the tab is hidden.
- A second tab that verifies sets `localStorage` `xf.email-verified`; the
  pending tab reacts to the `storage` event as if focus returned.
- Email validation: non-empty, contains `@` with text on both sides, at most
  254 characters. Validation runs on submit and on blur after the field was
  touched; the server remains authoritative.
- On a failed submit focus moves to the first invalid field. On a server error
  focus moves to the banner (`tabindex=-1`), then Tab continues to the actions.
- Keyboard: Enter submits the resend form. Esc does nothing. No view shortcuts;
  global shortcuts are off on auth pages.
- Pending screen focus order: header controls, `h1` (not a tab stop), resend
  button, different-account link.

## States

State scope: the auth card. The header and optional hero are always shown.

### Ready

- Pending: mail icon, title, text with the masked email, enabled resend button
  when the cooldown is over, different-account link.
- Verifying: spinner and status line while the token request runs.
- Resend: email field prefilled when `?email=` is valid, submit enabled.
- Success: check icon and the appropriate single primary action.

### Loading

- Page boot on the pending screen (`GET /me`): the card skeleton appears after
  200 ms (title bar, two text bars, button bar), `aria-busy` on the card, no
  text. Timeout 10 s becomes Error.
- Verifying the link: spinner plus `auth.verify.verifying.text`, card
  `aria-busy`, status `role=status`.
- Resending: button spinner and label `auth.verify.resend.sending`; the email
  field is read-only (not disabled, so its value stays readable); the
  different-account link is disabled.
- A slow call over 10 s keeps the spinner and adds `auth.verify.loading.slow`
  under the button.

### Empty

- `/verify-email` without a token, or with an empty token: link icon, title
  `auth.verify.empty.title`, text `auth.verify.empty.text`, action
  `auth.verify.empty.cta` (Send a new link).
- `/verify-email/pending` with no email in `GET /me` (defensive): the same empty
  block with the title `auth.verify.empty.no-account` and a sign-in action.
- This is not an error: no request failed and no security decision is implied.

### Error

- Invalid token (400 `invalid_token`): bad banner
  `auth.verify.error.invalid` and action `auth.verify.error.cta`.
- Expired token (410 `token_expired`): bad banner
  `auth.verify.error.expired` and action `auth.verify.error.cta`. The copy says
  when a link expires; it never shows the raw token.
- Already verified (409 `already_verified`): this is shown as Success, not as a
  failure, with `auth.verify.success.already` and a sign-in action.
- Resend network or 5xx, also a config failure that is not 404: bad banner
  `auth.verify.error.server` with `common.retry` (`auth-verify-error-retry`).
  The form values are kept. The same copy is used for timeouts.
- Rate limited (429): warning banner `auth.verify.error.rate-limited` with the
  minutes left; the button is disabled until the countdown ends.
- Invalid email on the resend form: field error
  `auth.verify.validation.email-required` or
  `auth.verify.validation.email-invalid`, linked with `aria-describedby`.
- Field errors are never shown as a toast. A failed resend does not reveal
  whether the account exists.

### Forbidden

- `403 wrong_user`: the link belongs to another account. Lock icon, title
  `auth.verify.forbidden.title`, text `auth.verify.forbidden.text`, action
  `auth.verify.forbidden.cta` (Sign in). Do not show the other account's email.
- `403 account_disabled`: the same forbidden card with
  `auth.verify.forbidden.disabled` and a sign-in action; the copy does not
  confirm whether the account exists.
- `403 csrf_rejected`: refresh the token with `GET /auth/config` and resend
  once automatically. A second 403 becomes the server error banner.
- `/verify-email/pending` without a session is not a forbidden page; the shell
  sends the person to `/login?reason=expired`.
- A signed-in user opening `/verify-email/resend` sees the signed-in variant,
  not a 403.

### Success

- Verified now: check icon, title `auth.verify.success.title`, text
  `auth.verify.success.text`, one primary action. Announce the success once
  through the card's `role=status` region.
- Already verified: same card, title `auth.verify.success.already-title`, text
  `auth.verify.success.already`, sign-in action.
- Resend accepted: neutral sent state, title `auth.verify.resend.sent-title`,
  text `auth.verify.resend.sent` with the email, expiry note
  `auth.verify.resend.expires` with `verification_ttl_hours`, a disabled
  resend-again button until the cooldown ends, and a back-to-sign-in action.
- Success never redirects automatically before the person can read the copy; a
  redirect happens only after the person activates the primary action.

### Partial and stale

If only `GET /auth/config` fails but `GET /me` answered, the pending screen is
shown with safe defaults (resend cooldown 60 s, link life 24 h) and a muted
line `auth.verify.config.partial`. The config is cached in memory for the page
lifetime only, never in `localStorage`.

### Session expired

A 401 from `GET /me` on `/verify-email/pending` is handled by the shell: stop
timers, show the session-expired modal, then `/login?reason=expired` with
`return_to=/verify-email/pending`. Unsaved resend input is kept in memory by
the shell and restored after sign in. The link and resend screens are public
and do not show the expired modal.

## Live updates

No SSE. The pending screen uses a 10 s poll of `GET /me` plus window focus,
the `storage` event and `online` recovery. Polling stops when the tab is hidden
and resumes with one immediate fetch. The link and resend screens make no
background requests.

## Responsive

- >= 900 px: two columns (hero and card), card vertically centred.
- 600 to 899 px: one column, card centred, hero hidden.
- < 600 px (390 px reference): full-width card with 16 px padding, no border or
  shadow; header stays one row; buttons are full width and 44 px high; inputs
  use 16 px text so iOS does not zoom. The on-screen keyboard scrolls the
  focused field into view. Reflows at 320 px without loss.

## Accessibility

- One `h1` per screen. The card is a `form` only on the resend screen; the other
  screens use a section with a labelled heading.
- The verifying line and success copy use `role=status` (`aria-live=polite`).
  Failure banners use `role=alert`. The resend countdown is announced at the
  start and every 60 s, not every second.
- Focus order: header controls, card controls. On success, focus moves to the
  success heading (`tabindex=-1`). On error, focus moves to the banner heading;
  on a second retry failure focus returns to the banner.
- Every field has a visible `<label for>`; required fields say "required" in
  text. `autocomplete="email"` and `inputmode="email"` on the email field.
  Paste and password managers are never blocked.
- The masked email is text, not an image, and is read with the copy. The token
  is never placed in an `aria-label`, a title or a log.
- Targets >= 44 px on mobile and >= 40 px on desktop. Visible 2 px accent focus
  ring with 2 px offset. Contrast >= 4.5:1 in light and dark. Reduced motion
  stops the skeleton shimmer and spinner rotation (static ring, text remains).
- `<html lang>` follows the language toggle and updates on switch.

## i18n

Keys follow `auth.verify.<area>.<element>`. EN is the source; VI exists for
every key. Placeholders are named. Dates, times and counts use the active
locale. Text expands 30 % and buttons wrap on 390 px rather than truncate.

| Key | EN | VI |
| --- | --- | --- |
| `auth.verify.pending.title` | Check your email | Kiểm tra email của bạn |
| `auth.verify.pending.text` | We sent a verification link to {email}. Open it to finish creating your account. | Chúng tôi đã gửi liên kết xác minh đến {email}. Hãy mở liên kết để hoàn tất tạo tài khoản. |
| `auth.verify.pending.resend` | Send a new link | Gửi liên kết mới |
| `auth.verify.pending.different` | Use a different account | Dùng tài khoản khác |
| `auth.verify.resend.title` | Send a verification link | Gửi liên kết xác minh |
| `auth.verify.resend.text` | Enter the email you registered with and we will send a new link. | Nhập email bạn đã đăng ký và chúng tôi sẽ gửi liên kết mới. |
| `auth.verify.resend.signed-in-text` | We will send a new link to {email}. | Chúng tôi sẽ gửi liên kết mới đến {email}. |
| `auth.verify.resend.submit` | Send link | Gửi liên kết |
| `auth.verify.resend.sending` | Sending… | Đang gửi… |
| `auth.verify.resend.cooldown` | You can ask again in {seconds} s. | Bạn có thể yêu cầu lại sau {seconds} giây. |
| `auth.verify.resend.ready` | You can ask for another link now. | Bạn có thể yêu cầu liên kết khác ngay bây giờ. |
| `auth.verify.resend.sent-title` | Check your email | Kiểm tra email của bạn |
| `auth.verify.resend.sent` | If an account exists for {email}, a new verification link is on its way. | Nếu tài khoản tồn tại với {email}, liên kết xác minh mới đang được gửi đến. |
| `auth.verify.resend.expires` | The link expires in {hours} hours. | Liên kết hết hạn sau {hours} giờ. |
| `auth.verify.resend.again` | Send again | Gửi lại |
| `auth.verify.back-to-signin` | Back to sign in | Quay lại đăng nhập |
| `auth.verify.verifying.title` | Verifying your email | Đang xác minh email của bạn |
| `auth.verify.verifying.text` | This only takes a moment. | Việc này chỉ mất một chút thời gian. |
| `auth.verify.success.title` | Email verified | Đã xác minh email |
| `auth.verify.success.text` | Your email is verified. You can now use XFlickr. | Email của bạn đã được xác minh. Bạn có thể dùng XFlickr ngay bây giờ. |
| `auth.verify.success.continue` | Continue | Tiếp tục |
| `auth.verify.success.signin` | Sign in | Đăng nhập |
| `auth.verify.success.already-title` | This email is already verified | Email này đã được xác minh |
| `auth.verify.success.already` | Nothing else is needed. Sign in to continue. | Không cần làm gì thêm. Hãy đăng nhập để tiếp tục. |
| `auth.verify.empty.title` | No verification link | Không có liên kết xác minh |
| `auth.verify.empty.text` | Open the link from your email, or ask for a new one. | Hãy mở liên kết trong email của bạn hoặc yêu cầu liên kết mới. |
| `auth.verify.empty.cta` | Send a new link | Gửi liên kết mới |
| `auth.verify.empty.no-account` | No account to verify | Không có tài khoản để xác minh |
| `auth.verify.forbidden.title` | This link belongs to another account | Liên kết này thuộc tài khoản khác |
| `auth.verify.forbidden.text` | Sign in with the account that received the email, then open the link again. | Hãy đăng nhập bằng tài khoản đã nhận email rồi mở lại liên kết. |
| `auth.verify.forbidden.disabled` | This account cannot be verified. Contact the administrator. | Không thể xác minh tài khoản này. Hãy liên hệ quản trị viên. |
| `auth.verify.forbidden.cta` | Sign in | Đăng nhập |
| `auth.verify.error.invalid` | This verification link is not valid. Ask for a new one. | Liên kết xác minh này không hợp lệ. Hãy yêu cầu liên kết mới. |
| `auth.verify.error.expired` | This verification link has expired. Ask for a new one. | Liên kết xác minh này đã hết hạn. Hãy yêu cầu liên kết mới. |
| `auth.verify.error.server` | We could not verify your email. Your link is still here. Check your connection and try again. | Không xác minh được email. Liên kết của bạn vẫn còn. Hãy kiểm tra mạng và thử lại. |
| `auth.verify.error.rate-limited` | Too many requests. Try again in {minutes} min. | Quá nhiều yêu cầu. Hãy thử lại sau {minutes} phút. |
| `auth.verify.error.cta` | Ask for a new link | Yêu cầu liên kết mới |
| `auth.verify.field.email` | Email | Email |
| `auth.verify.field.required` | required | bắt buộc |
| `auth.verify.validation.email-required` | Enter your email. | Hãy nhập email. |
| `auth.verify.validation.email-invalid` | Enter a valid email, like `name@example.com`. | Hãy nhập email hợp lệ, ví dụ `ten@example.com`. |
| `auth.verify.config.partial` | Some options could not load. You can still ask for a link. | Một số tùy chọn chưa tải được. Bạn vẫn có thể yêu cầu liên kết. |
| `auth.verify.loading.slow` | Still working… | Vẫn đang xử lý… |
| `common.retry` | Try again | Thử lại |
| `common.close` | Close | Đóng |
| `common.sign-in` | Sign in | Đăng nhập |

## Test IDs

Names follow patterns.md (`auth-verify-<element>`); repeated items use a stable
suffix.

- Page: `auth-verify-page`, `auth-verify-header`, `auth-verify-card`,
  `auth-verify-skeleton`.
- Pending: `auth-verify-pending`, `auth-verify-pending-title`,
  `auth-verify-email-masked`, `auth-verify-resend`,
  `auth-verify-resend-cooldown`, `auth-verify-different-account`.
- Link: `auth-verify-link-loading`, `auth-verify-success`,
  `auth-verify-success-continue`, `auth-verify-success-signin`,
  `auth-verify-error`, `auth-verify-error-cta`, `auth-verify-error-retry`,
  `auth-verify-empty`,
  `auth-verify-empty-cta`, `auth-verify-forbidden`,
  `auth-verify-forbidden-cta`.
- Resend: `auth-verify-resend-form`, `auth-verify-resend-email`,
  `auth-verify-resend-submit`, `auth-verify-resend-sent`,
  `auth-verify-resend-again`, `auth-verify-error-email`,
  `auth-verify-error-banner`, `auth-verify-error-retry`.
- Shared: `auth-verify-loading-slow`, `auth-verify-partial-note`.

## Test hooks

The `data-testid` list above is the contract for
`web/e2e/user-verify-email.spec.ts`. Minimum journeys: register, land on the
pending screen and open the link from Mailpit; verify and continue to
onboarding; expired link offers a new link; already-used link shows the
already-verified copy; resend with an unknown email shows the same neutral
message; wrong signed-in account shows the forbidden card; network failure
keeps the token and Retry succeeds; EN/VI switch; 390 px layout; pending screen
picks up verification from another tab.

## Out of scope

- Changing the email address, administrator resend or manual verification, and
  a real email provider.
- The shell banner and route guard for unverified users (user-shell) and the
  sign-in form itself (user-auth).
- OpenAPI edits and application code (only contract requests are recorded here).
