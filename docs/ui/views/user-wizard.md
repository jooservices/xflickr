# Setup wizard: API key, Flickr connection, more accounts

View id `user-wizard`, role USER, light gallery theme (dark optional), route
`/onboarding`. Baseline: `vOnboard()` in the approved
[POC](../poc/xflickr-poc.html) (the onboarding card with the photo mosaic).
Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md). Mockup:
[user-wizard.html](../mockups/user-wizard.html). Build ticket: JOOXF-167. The
Preview switch does not apply to this view.

## Purpose

Take a new person from an empty account to a running crawl in three steps: bring
their own Flickr API key (BYOK), authorize one Flickr account through OAuth,
then review the connected accounts, add more, choose the crawl depth and
start. It explains why XFlickr needs the person's own key (each key has its own
3,600 calls per hour) so nobody wonders where the key comes from. The same Add
API key modal is reused outside the wizard (Keys and quota, step 3).

## Route and access

- `/onboarding`, role USER (ADMIN may open it for their own account). The
  shell sends a signed-in user with no `valid` key and no Flickr account here
  once after login; anyone can come back later from Accounts or Keys.
  Unauthenticated: `/login?reason=expired&return_to=/onboarding`.
- Document title `Set up · XFlickr` (`user.wizard.title`). Focus moves to the
  step `h1` after every step change.
- The wizard has no rail and no tabs: the shell shows only the top bar (brand,
  EN/VI, theme, Sign out) so there is one thing to do.
- The step is derived from the server, not from the URL alone (see Data and
  API); `?step=` is only a hint after an OAuth return. A person who already has
  accounts opens step 3.
- Leaving mid-way loses nothing: the key is stored at step 1, accounts at
  step 2; reopening resumes at the right step.

## Layout

Split card, centered, max width 940 px, radius 22 px, as in the POC.

- Left (art): photo mosaic, decorative (`aria-hidden`), min height 440 px. The
  mockup draws it with colored tiles; the app uses the same justified grid as
  the POC with eighteen cached public tiles (`PHOTOS` slice). With no photos
  yet it shows the same tiles from the built-in palette. Never fetched from
  the network at this point.
- Right (body, padding 30): step bar, eyebrow `Step n of 3 · name`, `h1`, lead,
  the step content, then the actions row.
- Step bar: three 4 px segments (`steps` in the POC), filled up to the current
  step, plus the text eyebrow so state is not colour only. Done steps are not
  clickable; the Back button is the only way back.
- Mobile (<= 760 px): one column, the art becomes a 150 px strip above the
  body.

Step 1, Your API key (`wizard-step-key`):

- Explainer `callout` (info) "Why your own key?": XFlickr has no key; Flickr
  gives each key 3,600 calls per hour, so your crawl uses your own quota and
  never waits behind other people. Key and secret are stored encrypted and
  never shown again.
- Field API key (mono, 32 hex), field Secret (mono, `type=password`, show/hide
  `icon-btn`), links "Create a key on Flickr" (opens
  `flickr.com/services/apps/create` in a new tab), buttons Check key (primary)
  and Fill example (ghost, only in the demo build; hidden in production).

Step 2, Sign in (`wizard-step-connect`):

- Card `consent` as in the POC: header line with a lock and `flickr.com`, list
  of permissions asked (read: contacts, photos, favorites, albums; read
  non-public photos you can see, kept private), the line "It cannot upload,
  edit or delete anything", buttons Authorize on Flickr (primary), Cancel
  (ghost) and Back (to step 1, shown while idle).
- The real consent page is Flickr's, in the same tab. The mockup draws a
  simulated Flickr page (allow, deny, cancel) for review only; the app never
  imitates it.

Step 3, Connected (`wizard-step-done`):

- List of connected accounts (`list-item`: ok dot, username, mono nsid, line
  "Key {label} · read permission"), then Add another account (secondary) and
  Use another API key (ghost, opens the Add modal).
- Crawl depth: label with the live value, `range` slider 1 to `depth_max`,
  default 2 (never above the cap), help text with the meaning of each level.
- Primary Start crawling and go to Home; secondary note line "Contacts first,
  then photos, favorites, albums; sizes follow in their own lane."

## Data

Fed by the key list, the account list and the crawl settings; the first step
is chosen from them and each step writes through one call. Field-level sources
are in [Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Start step | `GET /keys` (exists), `GET /flickr-accounts` **contract request** (list for the current user, also requested by `user-keys`) | keys: `id`, `label`, `status`; accounts: `id`, `key_id`, `nsid`, `username`, `status`, `created_at`. No `valid` key: step 1. Key but no account: step 2. Account: step 3 |
| Check key (step 1) | `POST /keys` (exists) body `{label, key, secret}`; the server runs `flickr.test.echo` and stores the key only when Flickr accepts | 201 `ApiKey` `{id, label, key_public, status}`; 400 `validation_failed` with `details.fields` (**contract request**, as in `user-keys`); 409 `key_duplicate` **contract request**; 422 `key_invalid`; 429 `rate_limited`; 503 `no_node_available` **contract request**; network error or timeout (8 s) |
| Start OAuth (step 2) | `POST /flickr-accounts/connect` (exists) body `{key_id, return_to}` | 200 `authorize_url` then `window.location`; 409 `key_not_valid` **contract request**; `return_to` is a **contract request**: enum `onboarding` or `accounts`, the server picks the path, never a free URL |
| OAuth return | `GET /flickr-accounts/callback` (exists) | the server redirects: success `/onboarding?step=3&connected={account_id}`; declined (`authorization_denied`) `/onboarding?step=2&error=denied`; `invalid_state` or expired `/onboarding?step=2&error=expired`. The redirect target depends on `return_to` (**contract request**) |
| Crawl settings (step 3) | `GET /me/crawl-settings` **contract request** | `depth` (the person's setting), `depth_default` (administrator default), `depth_max` (administrator cap) |
| Start crawl (finish) | `POST /crawl/start` **contract request** body `{depth}` | 202 `{started: true}`; the server stores `depth`, queues the first crawl for every connected account and publishes `crawl.started`; 422 `depth_out_of_range` (above `depth_max` or below 1); 409 `no_account` |
| Live updates | `GET /api/v1/events/stream` (SSE, shell connection) | step 3 only: `account.status` `{account_id, key_id, status}` |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

Decisions made here (the POC does not show them):

- Three steps, not four: the POC's separate "Done" step is merged into step 3,
  which already shows what was connected and what will happen.
- Step 1 uses the one existing call: `POST /keys` already runs
  `flickr.test.echo`. There is no separate "check" endpoint and no draft key,
  so a key that passes is stored once. The label is the default
  `user.wizard.key.default-label` ("My Flickr key"); it can be renamed later in
  Keys. The secret is never returned and never kept in the page after success.
- Client rules mirror the POC: API key exactly 32 hex characters, Secret
  exactly 16, surrounding spaces and line breaks trimmed. The server stays
  authoritative.
- Sharing records is not asked here. It stays off until the person turns it on
  in Sharing (`user-sharing`); the POC toggle is dropped so a privacy choice
  is not buried in setup.
- Add another account reuses the same key. A second Flickr login on the same
  key is allowed by the API. "Use another API key" opens the Add modal and then
  returns to step 2 with the new key. With several keys, step 2 shows a select
  "Sign in with key" (`wizard-key-select`) preselected to the newest `valid`
  key.
- Depth: the slider range is 1 to `depth_max`. The initial value is
  `min(depth, depth_max)` when the person has set one, else
  `min(depth_default, depth_max)`; the shipped default is 2. Depth 1 follows
  contacts; each further level adds owners of photos favorited at the prior
  level (SPEC, Crawl depth).
- Finish is one call. Go to Home only after 202; Home shows the first crawl
  starting (live by its own spec). The toast `user.wizard.done.toast` is
  shown on Home.
- Contract requests are only requested here; OpenAPI is changed by the build
  tickets. Unsafe calls need `X-CSRF-Token`.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Check key | form submit, step 1 | Client validation, then `POST /keys`; on 201 go to step 2 with `key_id`. Errors stay on step 1 (see Error) |
| Fill example | ghost button, demo build only | Fills a random valid-looking pair; no call |
| Authorize on Flickr | primary, step 2 | `POST /flickr-accounts/connect`; on 200 show "Waiting for Flickr…", then leave to `authorize_url` |
| Cancel | step 2 while waiting, or on Flickr | Waiting: abort the call and return to idle. On Flickr, closing the tab or Cancel leaves a request token that expires after 10 minutes; reopening the wizard shows step 2 idle |
| Allow | on Flickr | Redirect with `step=3&connected=`: step 3, the new account row is focused and announced |
| Deny | on Flickr | Redirect with `error=denied`: step 2 with the denied message and Try again; nothing was stored |
| Try again | denied, expired, start failed | Repeats Authorize on Flickr |
| Back | steps 2 and 3 | Step 1 (step 2) or step 2 (step 3, treated as Add another account); no data is lost |
| Add another account | step 3 | Step 2, idle, same key (or the selected key) |
| Use another API key | step 3 | Opens the Add API key modal; on success step 2 with the new key |
| Set depth | slider, step 3 | Local only, written by Start crawling |
| Start crawling | primary, step 3 | `POST /crawl/start`; on 202 `go('home')` and toast |
| Add API key (modal) | step 3 or Keys | Same fields and rules as step 1 plus Label (1 to 80 characters); `POST /keys`; on 201 close, toast `user.wizard.addkey.toast`, focus returns to the opener |
| Retry | error or stale banner | Refetch the failed requests only |

## Interactions

- Validation runs on submit and on blur of a touched field; after the first
  failed submit it re-runs on input. The submit button is never disabled for
  validation, only while sending.
- Key and secret are never written to logs, local storage or the URL; the
  secret toggle has `aria-pressed`. Both are cleared from memory after a 201
  and when the modal closes.
- Slider: arrow keys change by 1; the value is in label and `aria-valuetext`.
- Double submit is ignored while a request is in flight. Tab order is the
  visual order; Enter submits step 1; Esc closes the modal.

## States

### Ready

The step content of Layout, per step. Step 1 empty fields, step 2 idle, step 3
with at least one account. After a successful `POST /keys` a polite status
`user.wizard.key.ok` is announced and step 2 opens. A connected account row has
an ok dot and the text "Connected", never colour only.

### Loading

First load: after 150 ms (200 ms per patterns) show the skeleton inside the
card: the art stays, the body shows a step bar, a heading bar, two text lines
and a field block; `aria-busy` on the body, one `role=status` line
`user.wizard.loading` and no spinner text. Timeout 10 s then Error. Step 1
submit and Start crawling keep the form and show the button spinner. Redirect
back from Flickr shows the same skeleton while accounts reload.

### Empty

Step 3 reached with no account (the account was removed in another tab, or the
list came back empty): `empty-block` with the user icon, title
`user.wizard.empty.title`, text `user.wizard.empty.text` and the call to
action `user.wizard.empty.cta` ("Connect Flickr account", `wizard-empty-cta`),
which opens step 2. Start crawling is hidden. Step 1 with no keys is the
normal Ready state, not an empty state.

### Error

- Initial load failed (network, 5xx, timeout): `empty-block` with the alert
  icon, `user.wizard.error.title`, `user.wizard.error.text` and `common.retry`
  (`wizard-error-retry`). Focus moves to the block.
- Step 1 field format: message under the field with icon
  (`user.wizard.key.error.key-format`, `...secret-format`); first invalid field
  focused.
- Step 1 invalid key (`key_invalid`): banner (bad)
  `user.wizard.key.error.invalid`, key field `aria-invalid`, values kept,
  focus on the banner.
- Step 1 `key_duplicate`: field error `user.wizard.key.error.duplicate`.
- Step 1 network or timeout, 429, 5xx: banner `user.wizard.key.error.network`
  with Try again; values kept. `no_node_available`:
  `user.wizard.key.error.no-node`.
- Step 2 denied: inline `user.wizard.connect.denied` and Try again.
  Expired (`invalid_state`): `user.wizard.connect.expired`. Start failed (network,
  5xx, `key_not_valid`): banner `user.wizard.connect.error.start`.
- Step 3 Start crawling failed: banner `user.wizard.done.error.finish`, button
  stays; `depth_out_of_range` re-reads the settings and shows
  `user.wizard.done.error.depth`.
- Add modal: same messages as step 1 plus `user.wizard.addkey.error.label`.

### Partial and stale

- Accounts loaded, crawl settings failed: step 3 works with depth 2 and a warn `banner` `user.wizard.stale.settings` with `common.retry`
  (`wizard-stale-retry`); the finish call still validates the depth.
- Accounts older than 30 s while the stream is down: the account list is
  refetched on focus; if that fails the list stays with a warn banner
  `user.wizard.stale.accounts`.

### Forbidden

403 on the first load (disabled account or policy change): `empty-block` with a
lock icon, `user.wizard.forbidden.title`, `user.wizard.forbidden.text`, link
`user.wizard.forbidden.cta` back to `/`. No retry. `csrf_rejected` on a write
refreshes the token once and repeats; a second 403 shows the step's failure
banner.

## Live updates

Only step 3 is live. It listens on the shell's SSE connection for
`account.status` and updates the row (ok, reconnect required, revoked) in place;
a status that becomes `revoked` while on this step adds the row text
`user.wizard.done.account.revoked` and removes Start crawling only when no `ok`
account remains. Fallback when SSE is down: poll `GET /flickr-accounts` every
10 s with backoff 10, 20, 40, 60 s, pausing while the tab is hidden. Steps 1 and
2 are not live: step 2 leaves the page and returns through the callback redirect.
The polite live region announces the account name once when it appears.

## Responsive

| Width | Behaviour |
| --- | --- |
| >= 1101 | Split card 940 px, art left |
| 761 to 1100 | Split card fills the width, body padding 24 |
| <= 760 | One column; art is a 150 px strip; buttons full width, 44 px high |
| 390 | Eyebrow wraps, account rows stack, the Add modal is a bottom sheet with sticky footer; fields use 16 px text; no horizontal scroll down to 320 px or 200 % zoom |

## Accessibility

- One `h1` per step; the step bar is `role=progressbar` with `aria-valuenow`,
  `aria-valuemax=3` and `aria-valuetext` "Step 2 of 3: Sign in". The art is
  `aria-hidden`.
- Fields have labels and help text through `aria-describedby`; invalid fields
  `aria-invalid`; error banners are `role=alert`; the success announcement is
  polite.
- The Flickr hand-off warns before leaving ("You will leave XFlickr to approve
  on Flickr") in the button's description.
- Modal: `role=dialog`, `aria-modal`, labelled by the title, focus trapped,
  focus restored. Secret toggle has `aria-label` and `aria-pressed`.
- Contrast >= 4.5:1 in light and dark; reduced motion stops shimmer and spinner.
  Targets >= 40 px (44 px on mobile). Status never relies on colour only.

## i18n

Keys follow `area.view.element`; `{name}` is a placeholder.

| Key | EN | VI |
| --- | --- | --- |
| `user.wizard.title` | Set up | Thiết lập |
| `user.wizard.step` | Step {n} of {total} · {name} | Bước {n}/{total} · {name} |
| `user.wizard.step.key` | Your API key | Khóa API của bạn |
| `user.wizard.step.connect` | Sign in | Đăng nhập |
| `user.wizard.step.done` | Connected | Đã kết nối |
| `user.wizard.steps.aria` | Setup progress | Tiến trình thiết lập |
| `user.wizard.loading` | Loading setup… | Đang tải phần thiết lập… |
| `user.wizard.key.title` | Bring your own Flickr API key | Dùng khóa API Flickr của riêng bạn |
| `user.wizard.key.lead` | XFlickr has no key of its own. Create one on Flickr, then paste the key and secret. They are stored encrypted and used only for your crawl. | XFlickr không có khóa riêng. Hãy tạo một khóa trên Flickr rồi dán khóa và bí mật vào đây. Chúng được lưu mã hóa và chỉ dùng cho việc thu thập của bạn. |
| `user.wizard.key.why.title` | Why your own key? | Vì sao cần khóa riêng? |
| `user.wizard.key.why.text` | Flickr gives every key 3,600 calls per hour. With your own key your crawl uses your own quota and never waits behind other people. | Flickr cho mỗi khóa 3.600 lượt gọi mỗi giờ. Với khóa riêng, việc thu thập dùng hạn mức của bạn và không phải xếp sau người khác. |
| `user.wizard.key.create-link` | Create a key on Flickr | Tạo khóa trên Flickr |
| `user.wizard.key.field.key` | API key | Khóa API |
| `user.wizard.key.field.secret` | Secret | Bí mật |
| `user.wizard.key.help.key` | 32 characters, 0-9 and a-f | 32 ký tự, 0-9 và a-f |
| `user.wizard.key.help.secret` | 16 characters, 0-9 and a-f | 16 ký tự, 0-9 và a-f |
| `user.wizard.key.show` | Show secret | Hiện bí mật |
| `user.wizard.key.hide` | Hide secret | Ẩn bí mật |
| `user.wizard.key.fill` | Fill example | Điền ví dụ |
| `user.wizard.key.submit` | Check key | Kiểm tra khóa |
| `user.wizard.key.checking` | Checking with Flickr… | Đang kiểm tra với Flickr… |
| `user.wizard.key.ok` | Key accepted. | Đã chấp nhận khóa. |
| `user.wizard.key.default-label` | My Flickr key | Khóa Flickr của tôi |
| `user.wizard.key.error.key-format` | The key should be 32 characters, 0-9 and a-f. Copy it again from Flickr's app page. | Khóa phải gồm 32 ký tự, 0-9 và a-f. Hãy sao chép lại từ trang ứng dụng trên Flickr. |
| `user.wizard.key.error.secret-format` | The secret should be 16 characters, 0-9 and a-f. | Bí mật phải gồm 16 ký tự, 0-9 và a-f. |
| `user.wizard.key.error.invalid` | Flickr rejected this key. Check the key and secret on your Flickr app page. | Flickr từ chối khóa này. Hãy kiểm tra khóa và bí mật trên trang ứng dụng Flickr. |
| `user.wizard.key.error.duplicate` | This key is already in your account. | Khóa này đã có trong tài khoản của bạn. |
| `user.wizard.key.error.network` | We could not reach Flickr. Your input is kept. Try again. | Không kết nối được với Flickr. Nội dung bạn nhập vẫn được giữ. Hãy thử lại. |
| `user.wizard.key.error.no-node` | No healthy node can take a new key right now. Try again in a few minutes. | Hiện chưa có nút khỏe nào nhận khóa mới. Hãy thử lại sau vài phút. |
| `user.wizard.connect.title` | Connect your Flickr account | Kết nối tài khoản Flickr của bạn |
| `user.wizard.connect.lead` | XFlickr asks Flickr for read permission. You approve it on Flickr; XFlickr never sees your password. | XFlickr xin Flickr quyền đọc. Bạn đồng ý trên Flickr; XFlickr không bao giờ thấy mật khẩu của bạn. |
| `user.wizard.connect.perm.title` | XFlickr would like to | XFlickr muốn |
| `user.wizard.connect.perm.read` | Read your contacts, photos, favorites and albums | Đọc danh bạ, ảnh, mục yêu thích và album của bạn |
| `user.wizard.connect.perm.private` | Read non-public photos you can see (kept private to you) | Đọc ảnh không công khai mà bạn xem được (chỉ riêng bạn thấy) |
| `user.wizard.connect.perm.none` | It cannot upload, edit or delete anything. | XFlickr không thể tải lên, sửa hay xóa bất cứ thứ gì. |
| `user.wizard.connect.key-select` | Sign in with key | Đăng nhập bằng khóa |
| `user.wizard.connect.allow` | Authorize on Flickr | Cho phép trên Flickr |
| `user.wizard.connect.leave` | You will leave XFlickr to approve on Flickr. | Bạn sẽ rời XFlickr để đồng ý trên Flickr. |
| `user.wizard.connect.cancel` | Cancel | Hủy |
| `user.wizard.connect.back` | Back | Quay lại |
| `user.wizard.connect.waiting` | Waiting for Flickr… | Đang chờ Flickr… |
| `user.wizard.connect.denied` | You declined on Flickr. Nothing was connected. Try again when ready. | Bạn đã từ chối trên Flickr. Chưa có gì được kết nối. Hãy thử lại khi bạn sẵn sàng. |
| `user.wizard.connect.expired` | The Flickr sign-in expired. Start again. | Phiên đăng nhập Flickr đã hết hạn. Hãy bắt đầu lại. |
| `user.wizard.connect.error.start` | Could not start the Flickr connection. Try again. | Không bắt đầu được kết nối Flickr. Hãy thử lại. |
| `user.wizard.done.title` | You are connected | Bạn đã kết nối xong |
| `user.wizard.done.lead` | Contacts come first, then their photos, favorites and albums. Sizes for every photo follow in their own lane. | Danh bạ được thu thập trước, rồi đến ảnh, mục yêu thích và album. Kích thước ảnh theo sau ở làn riêng. |
| `user.wizard.done.accounts` | Connected Flickr accounts | Tài khoản Flickr đã kết nối |
| `user.wizard.done.account.meta` | Key {label} · read permission | Khóa {label} · quyền đọc |
| `user.wizard.done.account.ok` | Connected | Đã kết nối |
| `user.wizard.done.account.revoked` | Access was revoked. Connect again. | Quyền truy cập đã bị thu hồi. Hãy kết nối lại. |
| `user.wizard.done.add` | Add another account | Thêm tài khoản khác |
| `user.wizard.done.add-key` | Use another API key | Dùng khóa API khác |
| `user.wizard.done.depth.label` | Crawl depth: {depth} | Độ sâu thu thập: {depth} |
| `user.wizard.done.depth.help` | 1 = your contacts. 2 = plus owners of photos they favorited (default). Up to {max}. | 1 = danh bạ của bạn. 2 = thêm chủ các ảnh họ yêu thích (mặc định). Tối đa {max}. |
| `user.wizard.done.finish` | Start crawling and go to Home | Bắt đầu thu thập và về Trang chủ |
| `user.wizard.done.finishing` | Starting… | Đang bắt đầu… |
| `user.wizard.done.toast` | Crawling has started | Đã bắt đầu thu thập |
| `user.wizard.done.error.finish` | We could not start the crawl. Your settings are kept. Try again. | Không bắt đầu được việc thu thập. Thiết lập của bạn vẫn được giữ. Hãy thử lại. |
| `user.wizard.done.error.depth` | That depth is above the limit set by the administrator. Choose a lower value. | Độ sâu này vượt giới hạn do quản trị viên đặt. Hãy chọn giá trị thấp hơn. |
| `user.wizard.addkey.title` | Add API key | Thêm khóa API |
| `user.wizard.addkey.label` | Label | Nhãn |
| `user.wizard.addkey.note` | XFlickr checks the key with one flickr.test.echo call and places it on the least loaded healthy node. | XFlickr kiểm tra khóa bằng một lệnh gọi flickr.test.echo và đặt nó trên nút khỏe ít tải nhất. |
| `user.wizard.addkey.submit` | Add key | Thêm khóa |
| `user.wizard.addkey.toast` | Key added | Đã thêm khóa |
| `user.wizard.addkey.error.label` | Enter a label of 1 to 80 characters. | Hãy nhập nhãn từ 1 đến 80 ký tự. |
| `user.wizard.empty.title` | No Flickr account connected | Chưa kết nối tài khoản Flickr nào |
| `user.wizard.empty.text` | Connect at least one Flickr account so XFlickr knows whose contacts to crawl. | Hãy kết nối ít nhất một tài khoản Flickr để XFlickr biết thu thập danh bạ của ai. |
| `user.wizard.empty.cta` | Connect Flickr account | Kết nối tài khoản Flickr |
| `user.wizard.error.title` | Setup could not load | Không tải được phần thiết lập |
| `user.wizard.error.text` | Something went wrong while loading. Nothing was changed. | Đã có lỗi khi tải. Chưa có gì bị thay đổi. |
| `user.wizard.stale.settings` | Crawl limits could not be loaded. Depth 2 is used; the server checks the limit. | Không tải được giới hạn thu thập. Dùng độ sâu 2; máy chủ sẽ kiểm tra giới hạn. |
| `user.wizard.stale.accounts` | The account list may be out of date. | Danh sách tài khoản có thể chưa mới. |
| `user.wizard.forbidden.title` | You cannot set up this account | Bạn không thể thiết lập tài khoản này |
| `user.wizard.forbidden.text` | Your account does not have access to this page. Ask the administrator. | Tài khoản của bạn không có quyền vào trang này. Hãy hỏi quản trị viên. |
| `user.wizard.forbidden.cta` | Back to Home | Về Trang chủ |
| `common.retry` | Try again | Thử lại |
| `common.cancel` | Cancel | Hủy |

## Test IDs

Each name is a `data-testid` value, following patterns.md
(`wizard-<area>-<element>`). Tests select by role first and by test id when
text depends on language.

- Page: `wizard-page`, `wizard-art`, `wizard-steps`, `wizard-eyebrow`,
  `wizard-title`, `wizard-skeleton`, `wizard-live-region`.
- Step 1: `wizard-step-key`, `wizard-key-why`, `wizard-key-create-link`,
  `wizard-key-input`, `wizard-key-secret`, `wizard-key-secret-toggle`,
  `wizard-key-fill`, `wizard-key-submit`, `wizard-key-error`,
  `wizard-key-error-key`, `wizard-key-error-secret`.
- Step 2: `wizard-step-connect`, `wizard-key-select`, `wizard-connect-perms`,
  `wizard-connect-allow`, `wizard-connect-cancel`, `wizard-connect-back`,
  `wizard-connect-waiting`, `wizard-connect-denied`,
  `wizard-connect-expired`, `wizard-connect-error`, `wizard-connect-retry`.
- Step 3: `wizard-step-done`, `wizard-accounts`, `wizard-account-<nsid>`,
  `wizard-account-status-<nsid>`, `wizard-add-account`, `wizard-add-key`,
  `wizard-depth`, `wizard-depth-value`, `wizard-finish`, `wizard-done-error`.
- Add modal: `wizard-addkey-modal`, `wizard-addkey-label`,
  `wizard-addkey-key`, `wizard-addkey-secret`, `wizard-addkey-secret-toggle`,
  `wizard-addkey-submit`, `wizard-addkey-cancel`, `wizard-addkey-error`.
- States: `wizard-empty`, `wizard-empty-cta`, `wizard-error`,
  `wizard-error-retry`, `wizard-stale-banner`, `wizard-stale-retry`,
  `wizard-forbidden`, `wizard-forbidden-cta`.

## Test hooks

The list above is the contract for `web/e2e/user-wizard.spec.ts`. Minimum
journeys: first visit starts at step 1; invalid hex, `key_invalid`,
`key_duplicate` and network errors keep the form and its values; valid key
moves to step 2; Authorize on Flickr starts the connect call; return with
`step=3` shows the account; `error=denied` shows the message and Try again
works; Add another account loops to step 2; depth defaults to 2 and never
exceeds `depth_max`; Start crawling calls `POST /crawl/start` then lands on
Home; Add API key modal from step 3; resume at step 2 and step 3 on reload;
empty step 3; load error with retry; 403 forbidden; EN/VI switch; 390 px layout
without horizontal scroll.

## Out of scope

- Editing, pausing or removing keys (`user-keys`); listing, disconnecting and
  reconnecting accounts later (`user-accounts`); the OAuth callback page when
  reached outside the wizard (`user-account-connect`).
- Turning on record sharing (`user-sharing`) and the administrator defaults
  that set `depth_default` and `depth_max` (`admin-defaults`).
- Crawl progress after Start (Home and `user-crawl`).
- OpenAPI edits (only requested above) and application code.
