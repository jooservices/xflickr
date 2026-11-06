# Two-factor sign-in

View id `user-two-factor`, role USER, light gallery theme (dark optional). Two
surfaces share one specification and one set of copy keys: the signed-in
management page `/security/two-factor` (inside the active user or admin shell)
and the public sign-in challenge `/login/2fa?challenge=...` (auth page frame
from [user-auth](user-auth.md)). Shared rules:
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-two-factor.html](../mockups/user-two-factor.html). Build ticket:
TBD (parent JOOXF-279).

## Purpose

Let a person protect the account with a time-based one-time password (TOTP),
enrol an authenticator by scanning a QR code or typing a key, save single-use
recovery codes, pass the challenge after the password step, and turn the second
factor off again. Recovery codes are the only fallback when the authenticator is
lost; they are shown once and stored hashed on the server.

## Route and access

- `/security/two-factor`: requires `xf_session`. 401 is handled by the shell
  (expired modal). The page renders inside the active shell, so admins use the
  admin shell and users the user shell; the content and test ids are shared.
  Entry point: Profile or the account menu, Security section. The build ticket
  adds the link; this view owns the destination.
- `/login/2fa?challenge=<id>`: public. It is reachable only with the short-lived
  `xf_2fa` cookie set by `POST /auth/login` when the account has two-factor on.
  A missing, expired or mismatched challenge redirects to
  `/login?reason=2fa-expired` with the info banner; the password was already
  accepted, so the person only signs in again.
- An already signed-in user who opens `/login/2fa` is redirected to the
  validated `return_to`, else `/` (`/admin` for admins).
- `return_to` follows the same validation as user-auth. The challenge carries
  it in the query and the successful verify call returns the person to it
  (replace, so Back does not return to the challenge).
- Feature flag `two_factor_enabled` (admin feature flags) hides the management
  entry and new enrolments when off; existing enrolments still verify until the
  flag is turned back on, so an administrator cannot lock users out by accident.
- Theme: management follows the shell role; the challenge uses
  `data-role="user"` and the auth header. Document titles:
  `Two-factor sign-in · XFlickr` and `Two-step verification · XFlickr`.

## Layout

### Management page

Page head: `h1` `two-factor.title`, a breadcrumb back to Profile or the admin
account page, and no page actions. One centred column, max width 720 px.

- Status card: `pill` `two-factor.status.on` or `two-factor.status.off`;
  method `two-factor.method.totp` when on; `two-factor.meta.enabled` with the
  date; `two-factor.meta.recovery` with the remaining count; primary action
  `two-factor.setup` when off; `two-factor.regenerate` and
  `two-factor.disable` (destructive) when on.
- Off (first use): `empty-block` with shield icon, title
  `two-factor.empty.title`, text `two-factor.empty.text`, action
  `two-factor.empty.cta` (Set up two-factor).
- Enrolment wizard, three steps in one card. The step indicator is a plain
  ordered list in text ("1 Scan · 2 Verify · 3 Save codes") with
  `aria-current="step"`; it is not a tablist.
  1. Scan: a 200 × 200 QR code on a surface with a quiet zone, the
     `two-factor.qr.alt` accessible name, the manual key in mono
     `two-factor.qr.key` with a copy button `two-factor.qr.copy`, and a
     disclosure `two-factor.qr.manual` ("Can't scan? Enter this key manually").
  2. Verify: one 6-digit code field (`inputmode=numeric`,
     `autocomplete=one-time-code`, `maxlength=6`, one input, not six boxes),
     the label `two-factor.code.label`, and the primary `two-factor.code.submit`.
     A back link returns to step 1 and keeps the same pending secret.
  3. Save codes: the 10 recovery codes in a two-column list of `<code>`
     elements, actions `two-factor.codes.copy`, `two-factor.codes.download` and
     `two-factor.codes.print`, the required checkbox `two-factor.codes.saved`,
     and the primary `two-factor.codes.finish` (disabled until the checkbox is
     checked). The warning `two-factor.codes.warning` states that the codes are
     shown once.
- On (ready): status card with a muted line `two-factor.meta.last-used` when
  known, `two-factor.regenerate` (sm) and `two-factor.disable` (sm, destructive
  ghost).
- Dialogs: `modal` for disable, `modal` for regenerate. Both require the
  current password. Disable also requires a current TOTP code or a recovery
  code so a stolen session alone cannot remove the factor.
- Components used: `card`, `button`, `text-field`, `checkbox`, `pill`, `banner`,
  `empty-block`, `modal`, `toast`, `skeleton`. No new component is introduced.

### Sign-in challenge

The auth frame is reused: header with brand and toggles, optional decorative
hero, one centred card (420 px). The card contains:

- `h1` `two-factor.challenge.title`, text `two-factor.challenge.text` with the
  masked email.
- One 6-digit code field as in the wizard, with `autocomplete=one-time-code`
  and a numeric keypad.
- Primary `two-factor.challenge.submit`.
- Link `two-factor.challenge.recovery-toggle` ("Use a recovery code instead").
  When open, the code field is replaced by a recovery-code field
  (`autocomplete=off`, `spellcheck=false`, `autocapitalize=characters`, mono)
  with the hint `two-factor.challenge.recovery-hint` and a link back to the
  authenticator code.
- Muted expiry line `two-factor.challenge.expires` with the minutes left.
- Footer link `two-factor.challenge.cancel` ("Back to sign in") which posts
  `POST /auth/2fa/cancel`, clears `xf_2fa` and returns to `/login`.

## Data

Short form: the management page reads the factor status and posts enrolment,
confirmation, regeneration and disable; the challenge reads the short-lived
challenge and posts one code. Fields and endpoints are in
[Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Page config | `GET /auth/config` (contract request from user-auth) | add `two_factor_enabled` (bool feature flag), `two_factor_challenge_ttl_minutes` (int) |
| Management status | `GET /me/two-factor` **contract request** | `enabled` (bool), `method` (`totp`), `confirmed_at` (ISO time, nullable), `last_used_at` (ISO time, nullable), `recovery_codes_remaining` (int) |
| Start enrolment | `POST /me/two-factor/enrol` **contract request** | 200 `{secret, otpauth_uri, expires_at}`; 409 `already_enabled`; 429 `rate_limited` |
| Confirm enrolment | `POST /me/two-factor/confirm` **contract request** body `{code}` | 200 `{recovery_codes: string[]}`; 400 `invalid_code` with `details.attempts_remaining`; 409 `already_enabled`; 410 `enrolment_expired`; 429 `rate_limited` |
| Regenerate codes | `POST /me/two-factor/recovery-codes` **contract request** body `{password}` | 200 `{recovery_codes: string[]}`; 401 `invalid_credentials`; 429 `rate_limited` |
| Disable | `POST /me/two-factor/disable` **contract request** body `{password, code}` | 204; 400 `invalid_code`; 401 `invalid_credentials`; 429 `rate_limited` |
| Password step | `POST /auth/login` (exists) | add 202 `two_factor_required` with `details.challenge_id`, `details.expires_at`, `details.methods` (`totp`, `recovery_code`) and `details.email_masked`; sets the short-lived `xf_2fa` cookie; 423 `account_locked`; 429 `rate_limited` |
| Challenge verify | `POST /auth/2fa/verify` **contract request** body `{challenge_id, code, method}` | 200 `User` and `xf_session` cookie; 401 `invalid_code` with `details.attempts_remaining`; 403 `account_disabled`; 410 `challenge_expired`; 423 `account_locked`; 429 `rate_limited` |
| Challenge cancel | `POST /auth/2fa/cancel` **contract request** body `{challenge_id}` | 204, clears `xf_2fa` |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

Recovery codes are 10 single-use values, generated by the server, shown once in
the response and stored only as hashes. The UI never re-requests or displays an
old code. The `otpauth_uri` contains the configured issuer and the account
email; the UI renders the QR from that URI and never invents an issuer.
Unsafe calls carry `X-CSRF-Token` equal to `xf_csrf`. Contract requests are only
requested here; OpenAPI changes belong to the build tickets.

## Configuration

Every operational value is an administrator setting, never a hard-coded
constant or an environment-only value. The build tickets add these to the
existing admin settings surface (`admin-defaults` and feature flags); the
screen reads them through the API or the returned payload.

| Setting | Default | Meaning |
| --- | --- | --- |
| `two_factor_enabled` | on | Feature flag for new enrolments and the management entry |
| `two_factor_issuer` | `XFlickr` | Issuer shown in the authenticator app |
| `two_factor_challenge_ttl_minutes` | 10 | Life of the `xf_2fa` challenge |
| `two_factor_enrolment_ttl_minutes` | 15 | Life of a pending enrolment secret |
| `two_factor_max_attempts` | 5 | Wrong codes before the challenge or step-up locks |
| `recovery_code_count` | 10 | Codes generated per set |
| `recovery_code_length` | 10 | Characters per recovery code |

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Set up two-factor | `two-factor.setup` or `two-factor.empty.cta` | `POST /me/two-factor/enrol`; show step 1 with the QR and manual key |
| Copy manual key | `two-factor.qr.copy` | Copy the base32 secret; toast `two-factor.copied.key` |
| Verify code | step 2 submit | `POST /me/two-factor/confirm`; on 200 show step 3 with the codes; on 400 show the field error and attempts left |
| Finish enrolment | `two-factor.codes.finish` | Close the wizard and show the On status; toast `two-factor.toast.enabled` |
| Copy, download or print codes | recovery-code actions | Copy all to the clipboard, download a `.txt` file, or open the print dialog; toast `two-factor.copied.codes` for copy |
| Regenerate codes | `two-factor.regenerate` | Modal asks for the password; `POST /me/two-factor/recovery-codes`; show the new codes once with the same save step |
| Disable | `two-factor.disable` | Modal asks for the password and a current TOTP or recovery code; `POST /me/two-factor/disable`; on 204 show Off and toast `two-factor.toast.disabled` |
| Challenge verify | challenge submit | `POST /auth/2fa/verify`; on 200 go to `return_to`; on 401 keep the code field and show attempts left |
| Use a recovery code | challenge toggle | Swap the field and `method=recovery_code`; keep the challenge and expiry |
| Cancel challenge | `two-factor.challenge.cancel` | `POST /auth/2fa/cancel`, clear the cookie, go to `/login` |
| Retry | error action | Repeat the failed request, show loading, focus the banner on a second failure |
| Switch language or theme | header toggles | Immediate, stored locally, `<html lang>` updated |

## Interactions

- The management page loads its status once and refetches on focus when the
  data is older than 60 s. Enrolment and dialogs are not live.
- Starting a new enrolment replaces any pending secret. If the pending secret
  expires, step 2 shows the expired error and a new-setup action; the QR is not
  reused.
- The 6-digit field accepts digits only, ignores spaces and hyphens on paste,
  and submits automatically only when the person presses Enter or activates the
  button; it never auto-submits on the sixth digit, so a screen reader can
  review the value. Paste is allowed.
- The recovery-code field accepts a code with or without the separator, is
  case-insensitive, and trims whitespace. It is single-use; a used code shows
  `two-factor.error.recovery-used` and does not count as a server error.
- Double submit is ignored while a request is in flight. The clicked button
  shows a spinner; other controls in the dialog are disabled.
- Wrong-code attempts show `two-factor.error.code` with
  `{remaining}` attempts. At zero the server answers 423/429 and the UI shows
  the lock countdown; the field is disabled until it ends.
- The challenge expiry line counts down locally from `expires_at`, updates once
  a minute and is announced at the start and every 60 s. At zero the screen
  shows `two-factor.error.expired` with a sign-in action; it never silently
  fails.
- Leaving the enrolment wizard before step 3 asks for confirmation through the
  shell navigation guard when a pending secret exists ("Your setup is not
  finished"). The secret is not kept in local storage.
- Disable and regenerate modals: focus starts on the password field, focus is
  trapped, Esc closes, the safe Cancel button is first for the destructive
  dialog, and focus returns to the invoking button. The destructive button is
  disabled until both fields have a value.
- The challenge is not dismissible with Esc because it is a page, not a modal;
  the cancel link is the explicit exit.
- Keyboard: Enter submits from a field. Space toggles the saved-codes checkbox.
  No view shortcuts; global shortcuts are off on auth pages. The QR image is
  not a tab stop; the manual key and copy button are.

## States

State scope: the surface content. The shell or auth header is always shown.

### Ready

- Management off: status Off, method none, empty block with setup action.
- Management on: status On, method Authenticator app, enabled date, recovery
  codes remaining, regenerate and disable actions.
- Enrolment step 1: QR, manual key, copy action, next step hint.
- Enrolment step 2: 6-digit field, submit, back link.
- Enrolment step 3: 10 recovery codes, copy/download/print, unchecked saved box,
  finish disabled.
- Challenge: masked email, 6-digit field, submit, recovery toggle, expiry line,
  cancel link.
- Challenge with recovery form: recovery-code field and hint, link back to the
  authenticator code, same submit and expiry.

### Loading

- Management boot: status card skeleton after 200 ms (pill bar, two text bars,
  button bar), `aria-busy`, no text. Timeout 10 s becomes Error.
- Starting enrolment: setup button spinner and `two-factor.enrolling`; the card
  is read-only.
- Confirming a code: submit spinner and `two-factor.verifying`; the code field
  is read-only.
- Regenerating or disabling: dialog button spinner and `two-factor.saving`;
  fields read-only; Esc is ignored while the request is in flight.
- Challenge verify: submit spinner and `two-factor.challenge.verifying`; the
  code field is read-only; the recovery toggle is disabled.
- A slow call over 10 s keeps the spinner and adds `two-factor.loading.slow`.

### Empty

- Management, factor off: shield icon, title `two-factor.empty.title`, text
  `two-factor.empty.text`, action `two-factor.empty.cta` (Set up two-factor).
  This is the normal first-use state, not a failure.
- Management, factor on but `recovery_codes_remaining = 0`: the status card
  adds the warning `two-factor.empty.no-codes` and the regenerate action; login
  by authenticator still works.
- The challenge has no empty state: a missing or expired challenge is an error
  that sends the person back to sign in.

### Error

- Status load failure (network, 5xx, timeout): inline bad `banner`
  `two-factor.error.load` with `common.retry` (`two-factor-error-retry`).
- Enrolment start failure: bad banner `two-factor.error.enrol`; the setup
  action stays enabled.
- Wrong code in step 2 or the challenge: field error
  `two-factor.error.code` with `{remaining}` attempts, `aria-invalid`, and the
  value kept so the person can correct it.
- Expired pending enrolment (410): banner `two-factor.error.enrol-expired` and
  the action `two-factor.setup` (Start again).
- Expired challenge (410): banner `two-factor.error.expired` and the action
  `two-factor.challenge.signin` (Sign in again). The `xf_2fa` cookie is cleared.
- Regenerate or disable failure: error inside the dialog
  `two-factor.error.action`; the dialog stays open and nothing changes.
- Wrong password in a dialog (401): field error `two-factor.error.password`,
  password cleared, code kept.
- Rate limited (429): warning banner `two-factor.error.rate-limited` with the
  minutes left; the submit button is disabled until the countdown ends.
- Network or 5xx on the challenge: bad banner `two-factor.error.server` with
  `common.retry`; the code is kept.
- Field errors are never shown as a toast.

### Forbidden

- Management without a session: the shell shows the session-expired modal and
  returns to `/login?reason=expired&return_to=/security/two-factor`.
- `403 forbidden` (the caller is not the account owner): the shell shows the
  full-page access-denied view; this page never accepts another user id.
- `403 csrf_rejected`: refresh the token with `GET /auth/config` and resend
  once automatically; a second 403 becomes the server error banner.
- Challenge without a valid `xf_2fa` cookie, with a challenge that belongs to a
  different browser, or after the cookie expired:
  `two-factor.challenge.forbidden` and the sign-in action. No account data is
  shown.
- `403 account_disabled` on the challenge: `two-factor.challenge.disabled`
  and the sign-in action; do not reveal whether the password was right.
- A challenge for a factor that was disabled in another tab is treated as an
  expired challenge, not as a successful sign-in.

### Success

- Enrolment complete: status On, method Authenticator app, recovery codes
  remaining 10, toast `two-factor.toast.enabled`, focus on the status heading.
- Challenge passed: the device goes to `return_to` (replace); the shell
  announces "Signed in" once through the polite region.
- Recovery code accepted: same success; the remaining count is decremented and
  the used code is never shown again.
- Codes regenerated: the new codes are shown once; on finish the status card
  updates the remaining count and the toast says `two-factor.toast.regenerated`.
- Disabled: status Off, toast `two-factor.toast.disabled`, focus on the status
  heading; existing sessions stay signed in, and the next sign-in has no
  challenge.

### Partial and stale

- The management status is cached in memory for the page lifetime only. A
  refetch on focus keeps the old status visible; if it fails, a muted line
  `two-factor.stale` with `Try again` appears at the top of the card.
- The challenge does not use cached data; it keeps the challenge id and expiry
  from the navigation state and validates them against the cookie on submit.
- If `GET /auth/config` fails, the challenge still works with the server
  response defaults; the management page hides the entry when the flag is
  unknown, never shows an unverified "on" state.

### Session expired

A 401 from `GET /me/two-factor` or any dialog action is handled by the shell:
stop timers, show the session-expired modal, and after sign in return to
`/security/two-factor`. The challenge has no session yet; its expiry and
`xf_2fa` cookie are handled by Forbidden.

## Live updates

No SSE. The management status refetches on focus only. The challenge uses a
local expiry timer; it does not poll. If another tab disables the factor, the
next challenge submit answers 410 and follows Forbidden.

## Responsive

- Management >= 900 px: one 720 px centred column. The two dialog fields sit
  side by side from 600 px and stack below.
- Challenge >= 900 px: two columns (hero and card). 600 to 899 px: one column,
  hero hidden.
- < 600 px (390 px reference): cards full width with 16 px padding; the QR
  scales to the available width but never below 180 px; recovery codes wrap to
  one column; dialog actions become full-width bottom-sheet actions; buttons are
  44 px high; inputs use 16 px text so iOS does not zoom. Reflows at 320 px
  without loss.

## Accessibility

- One `h1` per surface. The management card is a `section` labelled by its
  heading; the enrolment wizard moves focus to the heading of each step and
  sets `aria-current="step"` on the active step.
- The QR has `role="img"` and the accessible name `two-factor.qr.alt`; the
  secret is also plain text with a copy button, so the QR is never the only
  path. Never put the secret in the alt text, a title or a log.
- The 6-digit field is a single labelled `<input>` with `inputmode=numeric`,
  `autocomplete=one-time-code`, `maxlength=6` and a visible hint. It is not six
  separate boxes, so paste and screen readers work. Error and hint text are
  linked with `aria-describedby`; `aria-invalid` is set on failure.
- The recovery-code field has a visible label, `autocomplete=off`,
  `spellcheck=false` and a hint with the expected shape. It is not a password
  field, so it is readable while typing; the value is cleared after success.
- Recovery codes are a list of `<code>` elements in mono, not an image. The
  saved-codes checkbox has a visible label and must be checked before Finish.
  Copy announces through `role=status`; download and print are real buttons or
  links with names.
- Dialogs: `role=dialog`, `aria-modal=true`, labelled by the heading, focus
  trapped, Esc closes (unless a request is in flight), focus returns to the
  opener. The destructive action is not the initial focus.
- Errors: banners use `role=alert`; the attempt count and the expiry countdown
  use `role=status` and are announced at the start and every 60 s, not each
  second.
- Focus order: management header, status actions, wizard controls; challenge
  header, code field, submit, recovery toggle, cancel. Targets >= 44 px on
  mobile and >= 40 px on desktop. Visible 2 px accent focus ring with 2 px
  offset. Contrast >= 4.5:1 in light and dark.
- Reduced motion stops the skeleton shimmer and spinner rotation (static ring,
  text remains) and skips any wizard transition; the end state is shown at
  once.
- `<html lang>` follows the active language and updates on switch.

## i18n

Keys follow `two-factor.<area>.<element>`. EN is the source; VI exists for every
key. Placeholders are named. Dates, times and counts use the active locale. Text
expands 30 % and buttons wrap on 390 px rather than truncate.

| Key | EN | VI |
| --- | --- | --- |
| `two-factor.title` | Two-factor sign-in | Đăng nhập hai lớp |
| `two-factor.intro` | Add a second step after your password. | Thêm một bước thứ hai sau mật khẩu của bạn. |
| `two-factor.status.on` | On | Đang bật |
| `two-factor.status.off` | Off | Đang tắt |
| `two-factor.method.totp` | Authenticator app | Ứng dụng xác thực |
| `two-factor.meta.enabled` | Enabled {date} | Đã bật {date} |
| `two-factor.meta.recovery` | {count} recovery codes left | Còn {count} mã khôi phục |
| `two-factor.meta.last-used` | Last used {time} | Dùng lần cuối {time} |
| `two-factor.empty.title` | Two-factor sign-in is off | Đăng nhập hai lớp đang tắt |
| `two-factor.empty.text` | Add an authenticator app and a set of recovery codes. | Thêm ứng dụng xác thực và một bộ mã khôi phục. |
| `two-factor.empty.cta` | Set up two-factor | Thiết lập đăng nhập hai lớp |
| `two-factor.empty.no-codes` | You have no recovery codes left. Generate a new set. | Bạn đã hết mã khôi phục. Hãy tạo bộ mã mới. |
| `two-factor.setup` | Set up two-factor | Thiết lập đăng nhập hai lớp |
| `two-factor.enrolling` | Starting… | Đang bắt đầu… |
| `two-factor.verifying` | Verifying… | Đang xác minh… |
| `two-factor.saving` | Saving… | Đang lưu… |
| `two-factor.step.scan` | Scan | Quét mã |
| `two-factor.step.verify` | Verify | Xác minh |
| `two-factor.step.codes` | Save codes | Lưu mã |
| `two-factor.step.current` | Current step | Bước hiện tại |
| `two-factor.qr.alt` | QR code for the authenticator app | Mã QR cho ứng dụng xác thực |
| `two-factor.qr.help` | Scan this code with your authenticator app. | Quét mã này bằng ứng dụng xác thực của bạn. |
| `two-factor.qr.manual` | Can't scan? Enter this key manually | Không quét được? Hãy nhập khóa này thủ công |
| `two-factor.qr.key` | Setup key | Khóa thiết lập |
| `two-factor.qr.copy` | Copy key | Sao chép khóa |
| `two-factor.copied.key` | Setup key copied | Đã sao chép khóa thiết lập |
| `two-factor.code.label` | 6-digit code | Mã 6 chữ số |
| `two-factor.code.hint` | Enter the code shown in your authenticator app. | Nhập mã hiển thị trong ứng dụng xác thực. |
| `two-factor.code.submit` | Verify code | Xác minh mã |
| `two-factor.code.back` | Back to the QR code | Quay lại mã QR |
| `two-factor.codes.title` | Recovery codes | Mã khôi phục |
| `two-factor.codes.warning` | These codes are shown once. Store them somewhere safe. | Những mã này chỉ hiển thị một lần. Hãy lưu ở nơi an toàn. |
| `two-factor.codes.copy` | Copy all | Sao chép tất cả |
| `two-factor.codes.download` | Download | Tải xuống |
| `two-factor.codes.print` | Print | In |
| `two-factor.codes.saved` | I saved these codes | Tôi đã lưu các mã này |
| `two-factor.codes.finish` | Finish | Hoàn tất |
| `two-factor.copied.codes` | Recovery codes copied | Đã sao chép mã khôi phục |
| `two-factor.regenerate` | Generate new codes | Tạo mã mới |
| `two-factor.regenerate.title` | Generate new recovery codes? | Tạo mã khôi phục mới? |
| `two-factor.regenerate.text` | The old codes stop working. Enter your password to continue. | Mã cũ sẽ không dùng được. Hãy nhập mật khẩu để tiếp tục. |
| `two-factor.disable` | Turn off two-factor | Tắt đăng nhập hai lớp |
| `two-factor.disable.title` | Turn off two-factor sign-in? | Tắt đăng nhập hai lớp? |
| `two-factor.disable.text` | Your account will be protected by the password only. | Tài khoản của bạn sẽ chỉ được bảo vệ bằng mật khẩu. |
| `two-factor.disable.code` | Authenticator code or recovery code | Mã xác thực hoặc mã khôi phục |
| `two-factor.password.label` | Current password | Mật khẩu hiện tại |
| `two-factor.challenge.title` | Two-step verification | Xác minh hai bước |
| `two-factor.challenge.text` | Enter the 6-digit code for {email}. | Nhập mã 6 chữ số cho {email}. |
| `two-factor.challenge.submit` | Verify and sign in | Xác minh và đăng nhập |
| `two-factor.challenge.verifying` | Verifying… | Đang xác minh… |
| `two-factor.challenge.recovery-toggle` | Use a recovery code instead | Dùng mã khôi phục thay thế |
| `two-factor.challenge.recovery-hint` | Enter one of the recovery codes you saved. | Nhập một trong các mã khôi phục bạn đã lưu. |
| `two-factor.challenge.recovery-back` | Use the authenticator code | Dùng mã xác thực |
| `two-factor.challenge.expires` | This step expires in {minutes} min. | Bước này hết hạn sau {minutes} phút. |
| `two-factor.challenge.cancel` | Back to sign in | Quay lại đăng nhập |
| `two-factor.challenge.signin` | Sign in again | Đăng nhập lại |
| `two-factor.challenge.forbidden` | This sign-in attempt is no longer valid. | Lần đăng nhập này không còn hợp lệ. |
| `two-factor.challenge.disabled` | This account is disabled. Contact the administrator. | Tài khoản này đã bị vô hiệu hóa. Hãy liên hệ quản trị viên. |
| `two-factor.error.load` | Could not load two-factor settings. Check your connection and try again. | Không tải được cài đặt đăng nhập hai lớp. Hãy kiểm tra mạng và thử lại. |
| `two-factor.error.enrol` | Could not start the setup. Try again. | Không bắt đầu được thiết lập. Hãy thử lại. |
| `two-factor.error.enrol-expired` | This setup expired. Start again. | Thiết lập này đã hết hạn. Hãy bắt đầu lại. |
| `two-factor.error.code` | That code is not right. {remaining} attempts left. | Mã chưa đúng. Còn {remaining} lần thử. |
| `two-factor.error.recovery-used` | That recovery code was already used. | Mã khôi phục đó đã được dùng. |
| `two-factor.error.expired` | This sign-in attempt expired. Sign in again. | Lần đăng nhập này đã hết hạn. Hãy đăng nhập lại. |
| `two-factor.error.password` | The current password is not right. | Mật khẩu hiện tại chưa đúng. |
| `two-factor.error.action` | That did not work. Try again. | Thao tác chưa thành công. Hãy thử lại. |
| `two-factor.error.rate-limited` | Too many attempts. Try again in {minutes} min. | Quá nhiều lần thử. Hãy thử lại sau {minutes} phút. |
| `two-factor.error.server` | We could not reach XFlickr. Your code is kept. Check your connection and try again. | Không kết nối được XFlickr. Mã của bạn vẫn được giữ. Hãy kiểm tra mạng và thử lại. |
| `two-factor.toast.enabled` | Two-factor sign-in is on | Đăng nhập hai lớp đã bật |
| `two-factor.toast.disabled` | Two-factor sign-in is off | Đăng nhập hai lớp đã tắt |
| `two-factor.toast.regenerated` | New recovery codes created | Đã tạo mã khôi phục mới |
| `two-factor.stale` | Showing the last loaded settings, updated {time}. | Đang hiển thị cài đặt tải lần cuối lúc {time}. |
| `two-factor.loading.slow` | Still working… | Vẫn đang xử lý… |
| `two-factor.unsaved` | Your two-factor setup is not finished. Leave without finishing? | Thiết lập đăng nhập hai lớp chưa xong. Rời đi mà không hoàn tất? |
| `common.retry` | Try again | Thử lại |
| `common.cancel` | Cancel | Hủy |
| `common.close` | Close | Đóng |
| `common.sign-in` | Sign in | Đăng nhập |

## Test IDs

Names follow patterns.md. The management page uses `two-factor-<element>` and
the challenge uses `auth-two-factor-<element>`.

- Management page: `two-factor-page`, `two-factor-title`, `two-factor-card`,
  `two-factor-status`, `two-factor-setup`, `two-factor-regenerate`,
  `two-factor-disable`, `two-factor-empty`, `two-factor-empty-cta`,
  `two-factor-skeleton`, `two-factor-error`, `two-factor-error-retry`,
  `two-factor-stale-note`.
- Enrolment: `two-factor-step-scan`, `two-factor-qr`, `two-factor-key`,
  `two-factor-copy-key`, `two-factor-manual`, `two-factor-step-verify`,
  `two-factor-code`, `two-factor-code-submit`, `two-factor-code-error`,
  `two-factor-step-codes`, `two-factor-recovery-codes`,
  `two-factor-copy-codes`, `two-factor-download-codes`,
  `two-factor-print-codes`, `two-factor-codes-saved`,
  `two-factor-codes-finish`, `two-factor-enrol-error`.
- Dialogs: `two-factor-disable-dialog`, `two-factor-disable-password`,
  `two-factor-disable-code`, `two-factor-disable-confirm`,
  `two-factor-disable-cancel`, `two-factor-regenerate-dialog`,
  `two-factor-regenerate-password`, `two-factor-regenerate-confirm`,
  `two-factor-regenerate-cancel`.
- Challenge: `auth-two-factor-page`, `auth-two-factor-card`,
  `auth-two-factor-title`, `auth-two-factor-email`,
  `auth-two-factor-code`, `auth-two-factor-submit`,
  `auth-two-factor-recovery-toggle`, `auth-two-factor-recovery`,
  `auth-two-factor-recovery-back`, `auth-two-factor-expires`,
  `auth-two-factor-cancel`, `auth-two-factor-error`,
  `auth-two-factor-error-retry`, `auth-two-factor-forbidden`,
  `auth-two-factor-signin`.
- Notes: `two-factor-loading-slow`, `two-factor-unsaved-dialog`.

## Test hooks

The `data-testid` list above is the contract for
`web/e2e/user-two-factor.spec.ts`. Minimum journeys: enrol with a virtual
authenticator (QR and manual key), reject a wrong code with attempts left, save
the recovery codes and finish; sign out and pass the challenge with a TOTP
code; pass with a recovery code and see the remaining count drop; use an
already-used recovery code; expire the challenge and sign in again; disable
with the password and a code; regenerate codes and confirm the old set fails;
manage the factor from both shells; current password errors; rate limit and
lock countdown; network failure keeps the code; unsaved wizard guard; EN/VI
switch; 390 px layout.

## Out of scope

- Passkeys and WebAuthn (user-auth and user-profile), SMS or email one-time
  codes, trusted devices and "remember this browser", push approval, and
  administrator reset of another user's second factor.
- The sign-in password form (user-auth), the Profile password form
  (user-profile) and the shell session-expired modal (user-shell).
- OpenAPI edits and application code (only contract requests are recorded here).
