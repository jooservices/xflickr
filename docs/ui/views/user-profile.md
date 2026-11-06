# Profile

View id `user-profile`, role USER, light gallery theme (dark optional). One page
for everything a person controls about themselves: account, sign-in security
(password, passkeys, sessions), connected Flickr accounts and the danger zone.
Baseline: `vProfile()` in the [POC](../poc/xflickr-poc.html), which shows only
name, email, language, time zone, Save, Sign out and the Flickr account list;
everything else below is decided here. Shared rules:
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-profile.html](../mockups/user-profile.html). Build ticket:
JOOXF-166. Parent: JOOXF-64.

## Purpose

Let a signed-in user see and change their own details, keep their sign-in safe
(change the password, manage passkeys and social sign-in methods, end sessions
on other devices), manage which Flickr accounts are connected, and leave or
delete the account. It owns no photo data. This view links to the Flickr
accounts and Sessions cards. Social sign-in itself starts on
[user-auth](user-auth.md); this view links and unlinks the providers after the
person has proved ownership by signing in.

## Route and access

- `/profile` (also `/profile#security` and `/profile#sign-in-methods`).
  Opens inside the user shell, Profile tab active, document title
  `Profile · XFlickr`. A social link callback returns here with
  `?linked=<provider>` or `?link_error=<code>&provider=<id>`; the view reads it
  once, shows a toast or card banner, and removes the query parameters.
- Access: `xf_session`. 401 is handled by the shell (expired modal). `role =
  admin` is redirected to `/admin` by the shell; the admin account page is
  another ticket.
- Theme `data-role="user"`. The language select is the only place besides the
  shell toggle that writes `locale`; it is stored on the server (`PATCH /me`)
  and mirrored to `localStorage` `xf.locale`.
- Sensitive actions (change password, delete account, remove last passkey)
  re-ask the current password; a short grace is not kept.

## Layout

Page head: `h1` "Profile" (no actions). Below, a 12-column grid. Desktop (>=
1100 px): left column 5/12 (Account, Password), right column 7/12 (Passkeys,
Sign-in methods, Sessions, Flickr accounts); Danger zone spans full width at the
bottom. Below 1100 px one column in this order: Account, Flickr accounts,
Password, Passkeys, Sign-in methods, Sessions, Danger zone (most-used first on
small screens).

Every block is a `card` with an `h2` and its own states (they load and fail
independently).

1. **Account**: `avatar` 56 px (initials from display name, hue from `id`),
   display name as `h3` with email below (muted). Fields: Display name
   (`text-field`, 1 to 60 chars), Email (read-only `text-field`, help says it
   cannot be changed here), Language (`select` English / Tiếng Việt), Time zone
   (`select` of IANA names, searchable text input with datalist, default the
   browser zone). Footer row: `Save changes` (primary, enabled only when
   dirty) and `Sign out` (ghost, logout icon).
2. **Password**: three `text-field` (current, new, confirm) with show/hide, the
   rules list from user-auth (8 to 128 chars, different from email), `Change
   password` button. Help: "Other devices are signed out after you change it."
3. **Passkeys**: header row with title and `Add a passkey` (sm, primary) when
   the browser supports WebAuthn and `passkey_enabled`. List items: key icon,
   name (default "Passkey 1"), "Added {date} · Last used {date or never}",
   `Rename` and `Remove` (sm ghost). Hidden entirely when the server flag is
   off; shown with a muted note and no Add button when only the browser lacks
   support.
4. **Sign-in methods**: header row with the title and a muted intro
   ("Accounts you can use to sign in to XFlickr."). One `list-item` per
   configured provider in server order: a neutral initial mark and the provider
   name, status line
   (linked email and `Linked {date}`, `Not linked`, or `Not available`), and one
   action: `Unlink` (sm ghost) when linked, `Link {provider}` (sm) when not
   linked and enabled, nothing when unavailable. The card is hidden when no
   provider is configured. When unlinking would leave no password, no passkey
   and no other provider, `Unlink` is disabled and the row shows
   `profile.signin-methods.last`; a `Set a password` link moves focus to the
   Password card. No provider-specific row or copy exists; the provider name and
   mark come from the server list.
5. **Active sessions**: header row with `Sign out other sessions` (sm). List
   items: device icon, browser and OS ("Firefox on Linux"), masked IP,
   "Last active {relative}", "Signed in {date}", a `This device` pill on the
   current one (no revoke button there), `Revoke` (sm ghost) on the others.
6. **Flickr accounts**: header row with `Connect another` (sm primary, goes to
   `/accounts/connect`). List items: status dot (ok green check, warning for
   `reconnect_required`, bad for `revoked`), username, mono NSID, line
   "Signed in with {key label} · {permission} permission · since {date} ·
   {n} contacts", and `Disconnect` (sm ghost) or `Reconnect` (sm) for
   `reconnect_required` and `revoked`.
7. **Danger zone**: `card` with a bad-coloured 1 px border and tint. Row 1
   "Sign out everywhere" (also ends this session, goes to `/login`) with
   button `Sign out everywhere`; row 2 "Delete account" with a short consequence
   text and the destructive button `Delete account`.

Components used: `card`, `avatar`, `button`, `text-field`, `select`, `banner`,
`pill`, `list-item`, `modal` (confirm), `toast`, `skeleton`, `empty-state`. No
new component is introduced. Confirmations use the existing `modal` with the
destructive button variant.

## Data

Short form: one call per card, loaded in parallel on mount. Fields and
endpoints are in [Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Account | `GET /me` (exists) | `id`, `email`, `role`, `locale`, `created_at` |
| Account extras | **contract request** on `User` | add `display_name` (string, 1 to 60, default the part of email before `@`), `timezone` (IANA, default `UTC`), `passkey_enabled` (bool), `has_password` (bool; false for a social-only account) |
| Save account | `PATCH /me` **contract request** body `{display_name?, locale?, timezone?}` | 200 `User`; 400 `validation_failed` with `details.fields`; locale `en` or `vi` |
| Change password | `POST /me/password` **contract request** body `{current_password, new_password}` | 204, then the server revokes every other session; 400 `validation_failed`; 401 `invalid_credentials` (wrong current); 429 `rate_limited` with `Retry-After` |
| Passkeys list | `GET /me/passkeys` **contract request** | `items[]` `id`, `name`, `created_at`, `last_used_at` (nullable), `backed_up` (bool) |
| Add passkey | `POST /me/passkeys/options`, `POST /me/passkeys/verify` **contract request** | WebAuthn creation options and attestation; body adds `name`; 201 item; 409 `passkey_exists` |
| Rename / remove | `PATCH /me/passkeys/{id}` body `{name}`, `DELETE /me/passkeys/{id}` **contract request** | 200 item / 204; 404 means already gone (treated as success) |
| Sign-in methods | `GET /me/sign-in-methods` **contract request** | `has_password` bool, `items[]` `{provider, name, status: linked\|not_linked\|unavailable, email?, linked_at?, can_unlink, blocked_reason?: last_sign_in_method}` in server order |
| Link provider | `GET /auth/social/{provider}/start?mode=link&return_to=/profile` **contract request**, top-level navigation | Requires a session; 303 to the provider, then the callback attaches the identity and returns to `/profile?linked={provider}`; failures return `/profile?link_error=<code>&provider=<id>` (`identity_taken`, `denied`, `email_unverified`, `state_invalid`, `unavailable`, `failed`, `session_expired`) |
| Unlink provider | `DELETE /me/sign-in-methods/{provider}` **contract request** | 204; 404 not linked (treated as success); 409 `last_sign_in_method`; 403 `provider_unavailable`; unsafe, needs `X-CSRF-Token` |
| Sessions list | `GET /me/sessions` **contract request** | `items[]` `id`, `current` (bool), `user_agent_label` (server-parsed, e.g. "Firefox on Linux"), `ip_masked` (e.g. `203.0.113.x`), `created_at`, `last_seen_at`, `expires_at` |
| Revoke one | `DELETE /me/sessions/{id}` **contract request** | 204; revoking the current id is rejected 400 `cannot_revoke_current` |
| Revoke others | `DELETE /auth/sessions` (exists) | 204; 401; 403 `csrf_rejected` |
| Sign out | `POST /auth/logout` (exists) | 204, then client goes to `/login` |
| Flickr accounts | `GET /flickr-accounts` **contract request** (only connect and callback exist) | `items[]` `id`, `key_id`, `key_label`, `nsid`, `username`, `status` (`ok`, `reconnect_required`, `revoked`), `permission` (`read`), `contacts_count`, `created_at` |
| Disconnect | `DELETE /flickr-accounts/{id}` **contract request** | 204; 404 treated as success; 409 `crawl_running` carries `details.job_count` |
| Delete account | `DELETE /me` **contract request** body `{password}` | 204 clears cookies; 401 `invalid_credentials`; the server deletes the user, sessions, passkeys and Flickr tokens, and queues removal of crawled data |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

The sign-in methods list is the only public view of provider state. `can_unlink`
and `blocked_reason` are computed by the server from `has_password`, the passkey
count and the other linked providers, so the client never guesses whether an
unlink is safe. Link mode never creates an account; it only attaches a verified
identity to the signed-in user.

Contract requests are only requested here; OpenAPI is changed by the build
tickets. Unsafe calls carry `X-CSRF-Token` equal to `xf_csrf`. Until the
`DELETE /me` endpoint ships, the build hides the Delete account row (Sign out
everywhere stays).

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Save account | button, Enter in a field | Validate; `PATCH /me`; on 200 toast `profile.toast.saved`; a changed locale switches `<html lang>` and the UI immediately |
| Change password | submit | Validate; `POST /me/password`; on 204 clear the three fields, toast `profile.toast.password-changed`, reload Sessions (others are gone) |
| Add passkey | button | Options, `navigator.credentials.create`, ask a name (`modal` with a prefilled name), verify; user cancel shows nothing; failure shows `profile.passkeys.error.add` |
| Rename passkey | `Rename` | Inline text field replaces the name, Enter saves, Esc cancels |
| Remove passkey | `Remove` | Confirm modal; removing the last passkey asks the password; `DELETE`; toast |
| Link provider | `Link {provider}` | Full-page navigation to `GET /auth/social/{provider}/start?mode=link`; the row shows a spinner and all link actions are disabled; on return `?linked` shows the toast and reloads the card |
| Unlink provider | `Unlink` | Confirm modal `profile.signin-methods.unlink.title`; `DELETE /me/sign-in-methods/{provider}`; on 204 remove the link and toast; a 409 keeps the row and shows `profile.signin-methods.last` |
| Set a password | `Set a password` in a blocked row | Move focus to the Password card and its current-password field; no navigation |
| Revoke session | `Revoke` | Confirm modal `profile.sessions.revoke.title`; `DELETE /me/sessions/{id}`; item removed, toast |
| Sign out other sessions | button | Confirm modal; `DELETE /auth/sessions`; list reloads with only the current one; disabled when no other session exists |
| Connect another | button | Client route `/accounts/connect` |
| Disconnect account | `Disconnect` | Confirm modal listing consequences (photos stay, crawling for this account stops); `DELETE`; on 409 the modal shows `profile.accounts.error.running` and stays |
| Reconnect | `Reconnect` | Client route `/accounts/connect?reconnect={id}` |
| Sign out | button | `POST /auth/logout`, clear local caches, go to `/login` |
| Sign out everywhere | button | Confirm modal; `DELETE /auth/sessions`, then logout as above |
| Delete account | button | Modal: the user types their email and password; the destructive button stays disabled until the email matches; `DELETE /me`; on 204 go to `/login` with toast `profile.toast.deleted` |
| Retry | `Try again` in a card | Repeats only that card's request |

## Interactions

- Validation on submit and on blur after touch, then on input (as user-auth).
  Submit buttons are not disabled for validation, only while sending (Save is
  disabled while pristine, since there is nothing to send).
- Dirty guard: leaving `/profile` with unsaved Account or Password edits asks
  `profile.unsaved` through the shell navigation guard; reload shows the
  browser prompt.
- Language select previews immediately but persists only on Save; an
  unsaved change reverts when the user discards.
- Confirm modals focus the safe button (Cancel) first, trap focus, close on
  Esc and return focus to the invoking button; a removed item moves focus to
  the next item, else to the card heading.
- Double submit is ignored while a request is in flight; the clicked button
  shows a spinner.
- Link leaves the single-page app with a top-level navigation and stores
  nothing in `localStorage`; browser Back returns to Profile. On return the view
  reads `?linked` or `?link_error` once, shows the toast or the card banner, and
  removes the query parameters. A `link_error=session_expired` is handled by the
  shell like any other 401.
- The card never renders an unlink action for a provider whose `can_unlink` is
  false. It shows the reason and the `Set a password` shortcut instead, so the
  confirm modal cannot become a dead end.
- Date and relative times use the active locale; relative times update every
  minute while visible.
- Keyboard: no view shortcuts; global shortcuts of the shell apply.

## States

State scope: each card is independent; the page head is always shown.

### Ready

All cards loaded. Typical content: one account, a password form with empty
fields, 0 to 5 passkeys, one row per configured provider (linked, not linked
or unavailable; Google, Facebook, X and Apple when all four are enabled),
1 to 8 sessions (current first, then by `last_seen_at` descending), 1 to 3
Flickr accounts (oldest first). A social-only account shows
`has_password = false`, no password fields to fill in the Password card until
the person sets one, and at least one linked provider.

### Loading

Per card skeleton after 150 ms: avatar circle plus three field bars (Account);
three field bars (Password, shown at once since it needs no data); two list-item
bars (Passkeys, Sign-in methods, Sessions, Flickr accounts). `aria-busy` on the
card, no text. Timeout 10 s becomes that card's Error. Saving: button spinner
and label `profile.saving`; fields read-only. A slow call (over 10 s) adds
`profile.loading.slow`. Linking a provider shows the spinner in that row and
disables every other link action; the page leaves before a 10 s timeout unless
the browser blocks the navigation.

### Empty

- Passkeys, none added: icon, title `profile.passkeys.empty.title`, text
  `profile.passkeys.empty.text`, call to action `Add a passkey`.
- Flickr accounts, none connected: title `profile.accounts.empty.title`, text
  `profile.accounts.empty.text`, call to action `Connect a Flickr account`.
  Also shown when the user disconnects the last account.
- Sign-in methods is never empty while a provider is configured: unlinked
  providers are shown with `Link {provider}`. With no configured provider the
  whole card is hidden, not replaced by an empty state.
- Sessions is never empty (the current session is always listed). Account and
  Password have no empty state.

### Error

- Card load failure (network, 5xx, timeout): inline `banner` bad in that card,
  `profile.error.load` with the card name, button `Try again`. The other cards
  stay usable.
- Account save: field messages from `details.fields`; a general failure is a
  bad banner `profile.error.save` above the buttons, values kept.
- Wrong current password (401): field error `profile.password.error.current`,
  current field cleared. Rate limited (429): warning banner
  `profile.error.rate-limited`, button disabled for `Retry-After`.
- Passkey add or remove failure: `profile.passkeys.error.add` /
  `profile.passkeys.error.remove` as a card banner; browser cancel is silent.
- Link failure: the provider row returns to `not_linked` and the card shows a
  bad banner for the code: `identity_taken`, `denied` (info), `unverified`,
  `state_invalid`, `unavailable`, `failed` or `session_expired`. The row keeps
  its `Link {provider}` action so a retry is one click.
- Unlink failure: the row stays linked and the card shows
  `profile.signin-methods.error.unlink`; a 409 `last_sign_in_method` also
  disables the action and shows `profile.signin-methods.last`.
- Revoke or disconnect failure: toast (bad) `profile.error.action`, item stays.
- Disconnect while crawling (409): inside the modal
  `profile.accounts.error.running` with the job count.
- Delete failure: error inside the modal, nothing is deleted.

### Partial and stale

- Cards load independently; a failed card shows its Error while others stay
  Ready. The page never blanks because one call failed.
- Refetch on window focus when data is older than 60 s. During a refetch the old
  list stays visible; if it fails, a muted line `profile.stale` ("Showing the
  last loaded data, updated {time}") with `Try again` appears at the top of that
  card.
- A Flickr account with `status = reconnect_required` or `revoked` is shown with
  a warning or bad dot, the label `profile.accounts.status.*` and a `Reconnect`
  action; this matches the shell key-revoked banner.
- A provider with `status = unavailable` keeps its row with `Not available`
  and no action. Another tab linking or unlinking a provider is picked up on
  focus and on the `auth.identities` live event; an open confirm modal for a
  provider that changed state closes with `profile.signin-methods.changed`
  instead of sending a stale request.
- Another tab changing the same data is picked up by Live updates; an Account
  form with unsaved edits is never overwritten (a notice `profile.changed-elsewhere`
  offers `Reload` instead).

### Forbidden

- 401 on any call: the shell shows the session-expired modal; unsaved edits are
  kept for restore after sign in.
- 403 `csrf_rejected`: refresh the token (`GET /me`) and resend once; a second
  403 shows the card Error.
- 403 `forbidden` (role changed to admin while open): the shell shows the 403
  page. Profile never shows other users' data, so there is no per-item
  forbidden state. 403 `account_disabled` signs the user out to `/login`.

## Live updates

Live for Sessions, Flickr accounts and Sign-in methods. The shell's single SSE
connection carries topics `auth.sessions` (a session was created or revoked for
this user, for example from another device), `flickr.accounts` (status changed,
such as `reconnect_required`, or a connect finished in another tab) and
`auth.identities` (a provider was linked or unlinked in another tab). On an
event the card refetches its list. Fallback: when SSE is down the shell polls
these three endpoints every 30 s while the tab is visible (backoff 30, 60,
120 s), the Live indicator follows the shell. Account, Password and Passkeys
are not live; they refetch on focus. The current session being revoked
elsewhere arrives as a 401 and follows Forbidden.

## Responsive

- >= 1100 px: two columns plus full-width Danger zone, as in Layout.
- 600 to 1099 px: one column, max width 720 px, centred.
- < 600 px (390 px reference): one column, cards full width, 16 px side padding;
  list item actions drop below the text as a full-width row; sm buttons grow to
  44 px; header-row buttons of a card move under its title. Inputs use 16 px
  text (no iOS zoom). Modals become bottom sheets. Sign-in method rows put the
  provider name and status first, then a full-width `Link`/`Unlink` row; the
  last-method note wraps under the status. Reflows at 320 px without loss.

## Accessibility

- One `h1`; each card is a `section` labelled by its `h2` so the landmarks list
  works. Focus moves to the `h1` on route change (shell).
- Forms are native `<form novalidate>` with visible `<label for>`; errors linked
  by `aria-describedby`, `aria-invalid`, text plus icon. `autocomplete`:
  `name`, `current-password`, `new-password` so managers work; paste allowed.
- Save and action results are announced through the shell `aria-live` toast
  region; card errors are `role=alert`; stale and changed-elsewhere notes
  `role=status`.
- Lists are `ul`; every row action has an accessible name including the item
  ("Revoke session Firefox on Linux", "Remove passkey Laptop",
  "Link Google", "Unlink Google"). Status is text plus icon, never colour
  alone. `This device` is text. The provider mark is `aria-hidden` because the
  provider name is adjacent text.
- Sign-in method status is exposed as text (`Linked`, `Not linked`,
  `Not available`) and a disabled `Unlink` has `aria-describedby` pointing at
  the last-method note. The link and unlink results are announced through the
  shell toast region; link failures are a card `role=alert`.
- Modals: `role=dialog`, `aria-modal`, labelled, focus trap and restore. The
  destructive typed-email field has a visible label.
- Targets >= 44 px on mobile, >= 40 px on desktop. Contrast >= 4.5:1 in light
  and dark. Reduced motion stops skeleton shimmer and spinner rotation.

## i18n

Keys follow `profile.<area>.<element>`. EN is the source and VI exists for every
key. Placeholders are named. Text expands 30 %: buttons wrap on 390 px rather
than truncate. Dates and numbers use the active locale.

| Key | EN | VI |
| --- | --- | --- |
| `profile.title` | Profile | Hồ sơ |
| `profile.account.title` | Account | Tài khoản |
| `profile.account.name` | Display name | Tên hiển thị |
| `profile.account.email` | Email | Email |
| `profile.account.email-help` | You sign in with this email. It cannot be changed here. | Bạn đăng nhập bằng email này. Không thể đổi tại đây. |
| `profile.account.language` | Language | Ngôn ngữ |
| `profile.account.timezone` | Time zone | Múi giờ |
| `profile.account.save` | Save changes | Lưu thay đổi |
| `profile.account.signout` | Sign out | Đăng xuất |
| `profile.saving` | Saving… | Đang lưu… |
| `profile.password.title` | Password | Mật khẩu |
| `profile.password.current` | Current password | Mật khẩu hiện tại |
| `profile.password.new` | New password | Mật khẩu mới |
| `profile.password.confirm` | Confirm new password | Nhập lại mật khẩu mới |
| `profile.password.help` | Other devices are signed out after you change it. | Các thiết bị khác sẽ bị đăng xuất sau khi bạn đổi. |
| `profile.password.submit` | Change password | Đổi mật khẩu |
| `profile.password.error.current` | The current password is not right. | Mật khẩu hiện tại chưa đúng. |
| `profile.passkeys.title` | Passkeys | Passkey |
| `profile.passkeys.intro` | Sign in with your fingerprint, face or device lock instead of a password. | Đăng nhập bằng vân tay, khuôn mặt hoặc khóa thiết bị thay cho mật khẩu. |
| `profile.passkeys.add` | Add a passkey | Thêm passkey |
| `profile.passkeys.meta` | Added {date} · Last used {last} | Thêm ngày {date} · Dùng lần cuối {last} |
| `profile.passkeys.never` | never | chưa dùng |
| `profile.passkeys.rename` | Rename | Đổi tên |
| `profile.passkeys.remove` | Remove | Xóa |
| `profile.passkeys.name-label` | Passkey name | Tên passkey |
| `profile.passkeys.unsupported` | This browser cannot create passkeys. | Trình duyệt này không tạo được passkey. |
| `profile.passkeys.empty.title` | No passkeys yet | Chưa có passkey |
| `profile.passkeys.empty.text` | Add one to sign in without typing your password. | Thêm một passkey để đăng nhập mà không cần gõ mật khẩu. |
| `profile.passkeys.remove.title` | Remove passkey “{name}”? | Xóa passkey “{name}”? |
| `profile.passkeys.remove.text` | You will no longer be able to sign in with it. | Bạn sẽ không đăng nhập bằng nó được nữa. |
| `profile.passkeys.error.add` | The passkey could not be added. Try again. | Không thêm được passkey. Hãy thử lại. |
| `profile.passkeys.error.remove` | The passkey could not be removed. Try again. | Không xóa được passkey. Hãy thử lại. |
| `profile.signin-methods.title` | Sign-in methods | Cách đăng nhập |
| `profile.signin-methods.intro` | Accounts you can use to sign in to XFlickr. | Các tài khoản bạn có thể dùng để đăng nhập XFlickr. |
| `profile.signin-methods.meta` | {email} · Linked {date} | {email} · Đã liên kết {date} |
| `profile.signin-methods.not-linked` | Not linked | Chưa liên kết |
| `profile.signin-methods.unavailable` | Not available | Không khả dụng |
| `profile.signin-methods.link` | Link {provider} | Liên kết {provider} |
| `profile.signin-methods.unlink` | Unlink | Hủy liên kết |
| `profile.signin-methods.unlink.title` | Unlink {provider}? | Hủy liên kết {provider}? |
| `profile.signin-methods.unlink.text` | You will no longer sign in with {provider}. Your email and photos stay. | Bạn sẽ không đăng nhập bằng {provider} nữa. Email và ảnh của bạn vẫn còn. |
| `profile.signin-methods.last` | Keep at least one way to sign in. Add a password or link another provider first. | Hãy giữ ít nhất một cách đăng nhập. Thêm mật khẩu hoặc liên kết nhà cung cấp khác trước. |
| `profile.signin-methods.set-password` | Set a password | Đặt mật khẩu |
| `profile.signin-methods.error.link` | The {provider} account was not linked. Try again. | Chưa liên kết được tài khoản {provider}. Hãy thử lại. |
| `profile.signin-methods.error.identity-taken` | This {provider} account is already linked to another XFlickr account. | Tài khoản {provider} này đã liên kết với một tài khoản XFlickr khác. |
| `profile.signin-methods.error.denied` | {provider} sign-in was cancelled. Nothing changed. | Đăng nhập bằng {provider} đã bị hủy. Không có thay đổi nào. |
| `profile.signin-methods.error.unverified` | {provider} did not confirm your email address, so it was not linked. | {provider} chưa xác nhận địa chỉ email nên chưa được liên kết. |
| `profile.signin-methods.error.state-invalid` | This linking attempt expired. Try again. | Lần liên kết này đã hết hạn. Hãy thử lại. |
| `profile.signin-methods.error.unavailable` | {provider} linking is not available right now. Try again later. | Liên kết {provider} hiện không khả dụng. Hãy thử lại sau. |
| `profile.signin-methods.error.failed` | We could not finish linking {provider}. Try again. | Không hoàn tất liên kết {provider}. Hãy thử lại. |
| `profile.signin-methods.error.unlink` | The {provider} account was not unlinked. Try again. | Chưa hủy liên kết được tài khoản {provider}. Hãy thử lại. |
| `profile.signin-methods.changed` | This changed in another tab. Reload the card. | Mục này đã thay đổi ở tab khác. Hãy tải lại thẻ. |
| `profile.signin-methods.toast.linked` | {provider} linked | Đã liên kết {provider} |
| `profile.signin-methods.toast.unlinked` | {provider} unlinked | Đã hủy liên kết {provider} |
| `profile.sessions.title` | Active sessions | Phiên đăng nhập |
| `profile.sessions.intro` | Devices signed in to your account. | Các thiết bị đang đăng nhập tài khoản của bạn. |
| `profile.sessions.current` | This device | Thiết bị này |
| `profile.sessions.last-active` | Last active {time} | Hoạt động lần cuối {time} |
| `profile.sessions.signed-in` | Signed in {date} | Đăng nhập {date} |
| `profile.sessions.revoke` | Revoke | Thu hồi |
| `profile.sessions.revoke.title` | Sign out this device? | Đăng xuất thiết bị này? |
| `profile.sessions.revoke.text` | {device} will need to sign in again. | {device} sẽ cần đăng nhập lại. |
| `profile.sessions.revoke-others` | Sign out other sessions | Đăng xuất các phiên khác |
| `profile.sessions.revoke-others.text` | Every device except this one will be signed out. | Mọi thiết bị trừ thiết bị này sẽ bị đăng xuất. |
| `profile.accounts.title` | Flickr accounts | Tài khoản Flickr |
| `profile.accounts.connect` | Connect another | Kết nối thêm |
| `profile.accounts.meta` | Signed in with {key} · {permission} permission · since {date} · {count} contacts | Đăng nhập bằng {key} · quyền {permission} · từ {date} · {count} liên hệ |
| `profile.accounts.status.ok` | Connected | Đã kết nối |
| `profile.accounts.status.reconnect_required` | Reconnect needed | Cần kết nối lại |
| `profile.accounts.status.revoked` | Access revoked | Đã thu hồi quyền |
| `profile.accounts.reconnect` | Reconnect | Kết nối lại |
| `profile.accounts.disconnect` | Disconnect | Ngắt kết nối |
| `profile.accounts.disconnect.title` | Disconnect {username}? | Ngắt kết nối {username}? |
| `profile.accounts.disconnect.text` | Photos already saved stay. Crawling for this account stops. | Ảnh đã lưu vẫn còn. Việc thu thập cho tài khoản này sẽ dừng. |
| `profile.accounts.empty.title` | No Flickr account connected | Chưa kết nối tài khoản Flickr |
| `profile.accounts.empty.text` | Connect one to start keeping your circle. | Kết nối một tài khoản để bắt đầu lưu vòng kết nối của bạn. |
| `profile.accounts.empty.cta` | Connect a Flickr account | Kết nối tài khoản Flickr |
| `profile.accounts.error.running` | {count} crawl jobs are running for this account. Stop them first. | Đang có {count} tác vụ thu thập cho tài khoản này. Hãy dừng chúng trước. |
| `profile.danger.title` | Danger zone | Vùng nguy hiểm |
| `profile.danger.signout-all` | Sign out everywhere | Đăng xuất mọi nơi |
| `profile.danger.signout-all.text` | Ends every session, including this one. | Kết thúc mọi phiên, kể cả phiên này. |
| `profile.danger.delete` | Delete account | Xóa tài khoản |
| `profile.danger.delete.text` | Removes your account, passkeys and Flickr connections. This cannot be undone. | Xóa tài khoản, passkey và kết nối Flickr của bạn. Không thể hoàn tác. |
| `profile.danger.delete.confirm-email` | Type {email} to confirm | Nhập {email} để xác nhận |
| `profile.toast.saved` | Changes saved | Đã lưu thay đổi |
| `profile.toast.password-changed` | Password changed | Đã đổi mật khẩu |
| `profile.toast.deleted` | Your account was deleted | Tài khoản của bạn đã bị xóa |
| `profile.error.load` | Could not load {card}. Check your connection and try again. | Không tải được {card}. Hãy kiểm tra mạng và thử lại. |
| `profile.error.save` | Your changes were not saved. They are still here. | Thay đổi chưa được lưu. Nội dung vẫn còn ở đây. |
| `profile.error.rate-limited` | Too many attempts. Try again in {minutes} min. | Quá nhiều lần thử. Hãy thử lại sau {minutes} phút. |
| `profile.error.action` | That did not work. Try again. | Thao tác chưa thành công. Hãy thử lại. |
| `profile.stale` | Showing the last loaded data, updated {time}. | Đang hiển thị dữ liệu tải lần cuối lúc {time}. |
| `profile.changed-elsewhere` | This changed in another tab. | Mục này đã đổi ở tab khác. |
| `profile.unsaved` | You have unsaved changes. Leave without saving? | Bạn có thay đổi chưa lưu. Rời đi mà không lưu? |
| `profile.loading.slow` | Still working… | Vẫn đang xử lý… |
| `common.retry` | Try again | Thử lại |
| `common.cancel` | Cancel | Hủy |
| `common.reload` | Reload | Tải lại |

## Test IDs

Names follow patterns.md (`profile-<area>-<element>`); repeated items add the
entity id as suffix (`profile-session-row-{id}`).

- Page: `profile-page`, `profile-title`, `profile-unsaved-dialog`.
- Account: `profile-account-card`, `profile-account-avatar`,
  `profile-account-name`, `profile-account-email`, `profile-account-language`,
  `profile-account-timezone`, `profile-account-save`, `profile-account-signout`.
- Password: `profile-password-card`, `profile-password-current`,
  `profile-password-new`, `profile-password-confirm`, `profile-password-rules`,
  `profile-password-submit`, `profile-password-error`.
- Passkeys: `profile-passkeys-card`, `profile-passkeys-add`,
  `profile-passkey-row-{id}`, `profile-passkey-rename-{id}`,
  `profile-passkey-remove-{id}`, `profile-passkeys-empty`,
  `profile-passkeys-empty-cta`, `profile-passkeys-unsupported`.
- Sign-in methods: `profile-signin-methods-card`,
  `profile-signin-method-row-{provider}`,
  `profile-signin-method-status-{provider}`,
  `profile-signin-method-link-{provider}`,
  `profile-signin-method-unlink-{provider}`,
  `profile-signin-methods-last-{provider}`,
  `profile-signin-methods-set-password-{provider}`,
  `profile-signin-methods-error`, `profile-signin-unlink-dialog`,
  `profile-signin-unlink-confirm`, `profile-signin-unlink-cancel`.
- Sessions: `profile-sessions-card`, `profile-session-row-{id}`,
  `profile-session-current`, `profile-session-revoke-{id}`,
  `profile-sessions-revoke-others`.
- Flickr accounts: `profile-accounts-card`, `profile-accounts-connect`,
  `profile-account-row-{id}`, `profile-account-status-{id}`,
  `profile-account-disconnect-{id}`, `profile-account-reconnect-{id}`,
  `profile-accounts-empty`, `profile-accounts-empty-cta`.
- Danger: `profile-danger-card`, `profile-danger-signout-all`,
  `profile-danger-delete`, `profile-delete-email`, `profile-delete-password`,
  `profile-delete-confirm`.
- Dialogs and notes: `profile-confirm-dialog`, `profile-confirm-ok`,
  `profile-confirm-cancel`, `profile-stale-note`, `profile-changed-note`.
- States (per card, suffix with the card name: `account`, `password`,
  `passkeys`, `signin-methods`, `sessions`, `accounts`):
  `profile-skeleton-{card}`, `profile-error-{card}`,
  `profile-error-retry-{card}`.

## Test hooks

The `data-testid` list above is the contract for `web/e2e/user-profile.spec.ts`.
Minimum journeys: save name, language and time zone (UI switches to VI);
change password (wrong current, success, other sessions gone); add, rename and
remove a passkey (WebAuthn virtual authenticator); link a fake provider from
the Sign-in methods card and see the toast on return; unlink it; blocked
unlink with `last_sign_in_method` and the `Set a password` shortcut; each link
error code as a card banner; revoke one session and sign out the others;
disconnect an account, 409 while crawling, reconnect; empty passkeys and empty
accounts with their calls to action; per-card error and retry while the other
cards stay usable; SSE event refreshes Sessions, Flickr accounts and Sign-in
methods with polling fallback; delete account with typed email; unsaved guard;
390 px layout.

## Out of scope

- Password reset, email change, email verification, MFA other than passkeys,
  avatars upload, and notification preferences (none exist in this release).
- Signing in with a social provider and the first-time completion screen
  (`user-auth`); creating provider OAuth apps or setting their credentials
  (deployment configuration, never in the UI).
- Setting the Flickr API key or secret (entered only in the setup/keys UI,
  never here), key management (`user-keys`) and the connect flow itself
  (`user-account-connect`).
- Admin management of users (`admin-users`), OpenAPI edits (only requested
  above) and application code.
