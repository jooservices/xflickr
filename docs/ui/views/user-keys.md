# Keys and quota

View id `user-keys`, role USER, light gallery theme (dark optional), route
`/keys`. Baseline: `vKeys()` and `keyCard()` in the approved
[POC](../poc/xflickr-poc.html) (Keys & quota tab, USER role). Shared rules:
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-keys.html](../mockups/user-keys.html). Build tickets: JOOXF-176,
JOOXF-167. The view renders inside the [user shell](user-shell.md).

## Purpose

Let a person bring their own Flickr API keys and see, per key, how much of the
hourly quota is used, when it resets, which node and IP calls Flickr for it and
which Flickr accounts were authorized through it. They add, pause, resume and
remove keys here and reconnect accounts whose token stopped working. The view
explains the rules (3,600 calls per hour, reserve for user-started actions, one
node per key) so nobody has to guess why crawling paused.

## Route and access

- `/keys`, role USER (ADMIN may open it too and sees only their own keys; fleet
  wide quota is `admin-quota`). Unauthenticated: the shell sends the user to
  `/login?reason=expired&return_to=/keys`.
- Document title `Keys & quota · XFlickr` (`keys.title`). Focus moves to the
  `h1` after navigation.
- One user never sees another user's keys. The server enforces it; the UI
  never sends a user id.
- Deep links: `/keys?add=1` opens the Add modal (used by the shell banner
  "Reconnect" for a revoked key and by onboarding). The query is removed once
  the modal is closed.

## Layout

Inside the shell main column (max width 1200 px, 24 px padding, 16 px on
mobile).

- Page head: `h1` `keys.title`, lead text `keys.lead`, primary button
  `keys.add` (plus icon) aligned right; on mobile the button is full width
  under the lead.
- Key grid (`grid g2`): one `card` per key, 2 columns from 900 px, 1 below,
  16 px gap, ordered by `created_at` ascending (stable, never re-sorted by
  state so a card does not jump while it updates).
- Key card, top to bottom:
  1. Header: label (`h3`, one line, ellipsis, full text in `title`), masked key
     (mono, faint), at the right the state `pill` and, when paused, a `badge`
     `keys.paused`.
  2. Usage: text `Used 2,140 / 3,600` (`num`), then a `progress-bar` of
     used/cap. A tick on the bar marks where the reserve starts (cap minus
     reserve). Bar tone follows state: accent for `ok`, warn for `reserve`,
     bad for `exhausted`, muted for `revoked` or paused.
  3. Facts (`kv` list): Reserve (`keys.reserve.value`), Resets in (mm:ss
     countdown), Burst (`now / limit per second`), Egress (node id and IP).
  4. Lanes: two thin bars, Person lane and Sizes lane, each with its call
     count.
  5. Last 24 hours: 24 `hbars`, one per hour, red where the cap was reached.
  6. Flickr accounts connected to this key: a titled list (`keys.accounts.title`).
     One row per account: check or alert dot, `username`, mono `nsid`, status
     pill (`ok`, `reconnect_required`, `revoked`) and, for the last two, a
     `Reconnect` button. A key without accounts shows the muted line
     `keys.accounts.none` and a ghost button `keys.accounts.connect`.
  7. Footer actions: `Pause` / `Resume` (secondary, small) and `Remove` (ghost,
     danger, small).
- Foot of the page: info `banner` `keys.note` (more keys or nodes do not raise
  one key's limit; each key calls Flickr from one node).
- Components used: `card`, `pill`, `badge`, `progress-bar`, `kv` list (as in the
  POC), `modal`, `text-field`, `banner`, `empty-block`, `skeleton`, `button`,
  `toast`. No new component is introduced. Deviation from the POC, decided
  here: the POC `ring` is replaced by the `progress-bar` with a reserve tick,
  because the reserve zone is readable on a bar and the card stays one column
  wide on mobile.

## Data

The page is fed by the key list, the usage snapshot and the account list,
joined on `key_id`. A card renders once keys and usage are present; accounts
fill their own section. Field-level sources are in
[Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Key list | `GET /keys` (exists) | `id`, `label`, `key_public` (masked, first 6 characters then bullets), `per_hour_limit` (the cap), `per_second_limit`, `burst`, `status` (`valid`, `invalid`, `revoked`), `created_at` |
| Usage snapshot | `GET /keys/usage` **contract request** | `server_time`, `items[]` of `key_id`, `used`, `person_used`, `sizes_used`, `reserve`, `burst_now`, `window_resets_at`, `hourly[24]` (calls per past hour, oldest first), `node_id`, `node_ip`, `paused` bool, `state` (`ok`, `reserve`, `exhausted`, `revoked`) |
| Flickr accounts | `GET /flickr-accounts` **contract request** (list for the current user) | `id`, `key_id`, `nsid`, `username`, `status` (`ok`, `reconnect_required`, `revoked`), `created_at` |
| Add key | `POST /keys` (exists) body `{label, key, secret}` | 201 `ApiKey`; 400 `validation_failed`; 422 `key_invalid` (Flickr rejected the `flickr.test.echo` check); 429 `rate_limited` |
| Add key detail | **contract request** on `POST /keys` | 400 carries `details.fields` `{label?, key?, secret?}` each a code (`required`, `too_long`, `invalid_format`); new 409 `key_duplicate`; new 503 `no_node_available` when no healthy crawl node can take the key |
| Pause / resume | `PUT /keys/{id}` **contract request** adds `paused` (bool) to `UpdateKeyRequest` | 200 `ApiKey`; the usage `paused` and `state` follow |
| Remove key | `DELETE /keys/{id}` (exists) | 204; also deletes the key's accounts and stops queued work (`key.deleted`) |
| Reconnect | `POST /flickr-accounts/connect` (exists) body `{key_id}` | 200 `authorize_url`, then `window.location` to Flickr; 409 `key_not_valid` **contract request** when the key is not `valid` |
| Live updates | `GET /api/v1/events/stream` (SSE, shell connection) | channel `quota.keys`: event `key.quota` with the usage item above; `key.status` with `{key_id, status}`; `account.status` with `{account_id, key_id, status}` |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

Decisions made here (the POC does not show them):

- The cap shown is `per_hour_limit` of the key; the reserve is the server
  default (100) returned in the usage item; the secret is never shown or
  returned. The Add modal does not ask for limits: the administrator defaults
  apply.
- The state pill comes from usage `state`: `ok` below `cap - reserve`,
  `reserve` from there to the cap, `exhausted` at the cap, `revoked` when
  `status = revoked`. `status = invalid` shows a fifth pill `invalid` (tone bad,
  `keys.state.invalid`), because the key can recover to `valid` while `revoked`
  is terminal.
- Paused is a flag, not a state: the pill keeps showing the quota state and the
  `badge` says Paused. A paused key makes no scheduled calls; actions the user
  starts still use it.
- Revoked and invalid keys cannot be paused or resumed (button hidden) and show
  a warning line `keys.revoked.hint` pointing to Add API key. They can be
  removed.
- Client countdown: `window_resets_at - (server_time + elapsed since the
  response)`, never the browser clock alone, so skewed clocks do not lie.
- Contract requests are only requested here; OpenAPI is changed by the build
  tickets. Unsafe calls need `X-CSRF-Token`.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Open Add API key | button, `?add=1` | Opens the modal with focus in Label. Empty fields. |
| Add key | submit in modal | Client validation, `POST /keys`; on 201 close, toast `keys.toast.added` with the node, insert the card at the end, focus its `h3`. Errors stay in the modal (see Error) |
| Pause / Resume | card button | `PUT /keys/{id}` `{paused}`; the button shows a spinner and is disabled meanwhile; on 200 update the badge and bar, toast `keys.toast.paused` / `keys.toast.resumed` |
| Remove | card button | Opens the confirm modal `keys.remove.title`; confirm calls `DELETE /keys/{id}`; on 204 remove the card, toast `keys.toast.removed`, focus the Add button (or the empty block). Cancel and Esc close |
| Reconnect | account row button | `POST /flickr-accounts/connect` `{key_id}`; on 200 leave to Flickr. After Flickr the callback returns to `/accounts` (the Accounts view), not here |
| Connect account | ghost button on a key without accounts | Same call; label differs only |
| Retry | error or stale banner | Refetch the failed requests only |

## Interactions

- Validation runs on submit and on blur of a touched field; after the first
  failed submit it re-runs on input. The submit button is never disabled for
  validation reasons, only while sending.
- Client rules mirror the POC: Label 1 to 80 characters (trimmed); API key
  exactly 32 hex characters; Secret exactly 16 hex characters; surrounding
  spaces are trimmed before sending. The server stays authoritative.
- Pasted spaces and line breaks are cleaned. The secret field is
  `type=password` with a show/hide `icon-btn`; neither field is written to
  logs, local storage or the URL, and the modal clears them on close.
- Double submit and double click on Pause/Remove are ignored while a request is
  in flight.
- Adding a key: the modal text says the key is checked with one Flickr call and
  placed on the least loaded healthy node; the card appears with `0 / cap`.
- A new card shows `keys.accounts.connect` so the person connects an account.
- Keyboard: Tab order follows the card order (label has no stop, then Pause,
  Remove, then the account buttons); Esc closes a modal and returns focus to the
  button that opened it.

## States

### Ready

Cards as in Layout, live. Each card shows its own state pill, so a page can mix
`ok`, `reserve`, `exhausted` and `revoked`.

- `reserve`: the card gets a warn hint line `keys.reserve.hint` (only actions
  you start run now).
- `exhausted`: bad hint line `keys.exhausted.hint` with the reset time
  (`{time}` in the user locale); at zero the card turns `ok` by itself.
- Paused: badge, muted bar, hint `keys.paused.hint`.
- `revoked` / `invalid`: bar muted, usage shown as the last known numbers,
  hint `keys.revoked.hint`.

### Loading

First load: after 150 ms show the `skeleton`: page head stays, three grey card
blocks (header bars, a wide bar, four fact lines, an action row), `aria-busy`
on the grid, no spinner text. Cards appear together once `GET /keys` and
`GET /keys/usage` answered; accounts load later and their section shows two
shimmer lines meanwhile. Timeout 10 s then Error. Reloading data keeps cards
(no skeleton on refresh).

### Empty

`GET /keys` returned `[]`. `empty-block` with the key icon, title `keys.empty.title`
("No API key yet"), text `keys.empty.text` and the call to action
`keys.empty.cta` ("Add API key", `keys-empty-cta`) that opens the Add modal. The
page head button stays. The foot note is hidden. Usage and accounts are not
requested.

### Error

- List failed (network, 5xx, timeout): `empty-block` with alert icon, title
  `keys.error.title`, text `keys.error.text` and button `common.retry`
  (`keys-error-retry`). No cards. Focus moves to the block.
- Add modal, field validation: message under the field (text + icon):
  `keys.error.label-required`, `keys.error.key-format`, `keys.error.secret-format`;
  the first invalid field is focused.
- Add modal, `key_invalid`: banner (bad) in the modal `keys.error.key-invalid`,
  key field `aria-invalid`, secret kept, focus on the banner.
- Add modal, `key_duplicate`: field error on the key `keys.error.key-duplicate`.
- Add modal, `no_node_available`: banner `keys.error.no-node`.
- Add modal, 429 or 5xx: banner `keys.error.add-failed`; values are kept.
- Pause, Resume or Remove failed: toast (bad) `keys.error.action-failed`; the
  card is unchanged. A Remove 404 is treated as success.
- Reconnect failed: toast `keys.error.reconnect-failed`; the row stays.

### Partial and stale

- Keys loaded, usage failed: cards show label, masked key, limits and the
  account section; numbers show an en dash, the bar is empty and a warn
  `banner` `keys.stale.usage` (with `common.retry`, `keys-stale-retry`) heads
  the grid.
- Accounts failed: only the account section of each card shows
  `keys.accounts.error` with an inline retry; the rest stays.
- Data older than 30 s while live is off or the stream is down (two missed
  polls): the same stale banner with `{time}` of the last update; numbers stay
  readable and the countdown stops at `--:--` instead of guessing.
- A card whose key was removed elsewhere disappears on the next refresh with
  no toast.

### Forbidden

403 on `GET /keys` (disabled account, or an auth policy change): the grid is
replaced by an `empty-block` with a lock icon, `keys.forbidden.title`,
`keys.forbidden.text` and a link `keys.forbidden.cta` back to `/`. No retry.
403 `csrf_rejected` on an action refreshes the token once and repeats; a second
403 shows `keys.error.action-failed`.

## Live updates

The view is live. It uses the shell's single SSE connection (channel
`quota.keys` plus `account.status` and `key.status` events) and updates cards
in place: used count, bar, state pill, burst, node and IP, hourly bars,
account status. A changed card does not animate except a 600 ms accent
background on the changed number (`fresh`, off with reduced motion). Fallback
when SSE is down or Live is off: poll `GET /keys/usage` every 10 s (backoff 10,
20, 40, 60 s), poll `GET /flickr-accounts` every 30 s, pause when the tab is
hidden and fetch once on show. The reset countdown is a local 1 s timer; on
reaching 0 the view fetches usage once at once and again after 3 s. Events are
buffered while the user pressed Live paused, and on resume the view refetches.
A state change is announced once through the polite live region
(`keys.live.state-changed`), never every call.

## Responsive

| Width | Behaviour |
| --- | --- |
| >= 1101 | Two card columns, page head on one row |
| 900 to 1100 | Two columns, facts wrap to two lines inside the card |
| < 900 | One column; Add API key goes full width under the lead |
| 390 | Account rows stack: name and nsid on top, status and Reconnect below at 44 px height; card actions wrap; modals become bottom sheets with sticky footer buttons; fields use 16 px text; no horizontal scroll down to 320 px or 200 % zoom |

## Accessibility

- One `h1`; each card is an `article` labelled by its `h3`; the grid is a `ul`
  with `li` children so screen readers count the keys.
- The progress bar is `role=progressbar` with `aria-valuenow` (used),
  `aria-valuemax` (cap) and `aria-valuetext` "2,140 of 3,600 calls used";
  state is also in text via the pill, never colour only. The reserve tick has a
  text equivalent in the Reserve fact.
- The reset countdown is not a live region; the polite region announces state
  changes only (see Live updates).
- Pause/Resume are plain buttons whose name includes the key label (`aria-label` `keys.pause.aria`). Remove names the key too.
- Modals: `role=dialog`, `aria-modal`, labelled by the title, focus trapped,
  focus restored on close. Errors use `aria-describedby` and `aria-invalid`;
  the failure banner is `role=alert`. The secret toggle has `aria-label` and
  `aria-pressed`.
- Contrast >= 4.5:1 in light and dark; reduced motion stops the shimmer, the
  spinner and the `fresh` highlight. Targets >= 40 px (44 px on mobile).

## i18n

Keys follow `keys.<area>.<element>` (i18n.md). EN is the source; every key has
a VI text. Numbers, times and countdowns use the active locale. Text grows by
30 %: buttons wrap, labels never truncate silently.

| Key | EN | VI |
| --- | --- | --- |
| `keys.title` | Keys & quota | Khóa & hạn mức |
| `keys.lead` | You bring your own Flickr API key. Each key allows 3,600 calls per hour and about 4 per second. The last 100 calls are kept for actions you start, like a download. | Bạn dùng khóa API Flickr của riêng mình. Mỗi khóa cho phép 3.600 lượt gọi mỗi giờ và khoảng 4 lượt mỗi giây. 100 lượt cuối được giữ cho các thao tác bạn bắt đầu, như tải xuống. |
| `keys.add` | Add API key | Thêm khóa API |
| `keys.paused` | Paused | Đã tạm dừng |
| `keys.state.ok` | OK | Bình thường |
| `keys.state.reserve` | Reserve | Dự trữ |
| `keys.state.exhausted` | Exhausted | Hết hạn mức |
| `keys.state.revoked` | Revoked | Đã thu hồi |
| `keys.state.invalid` | Invalid | Không hợp lệ |
| `keys.used` | Used | Đã dùng |
| `keys.used.value` | {used} / {cap} calls | {used} / {cap} lượt gọi |
| `keys.used.aria` | {used} of {cap} calls used | Đã dùng {used} trên {cap} lượt gọi |
| `keys.reserve` | Reserve | Dự trữ |
| `keys.reserve.value` | {count} calls for actions you start | {count} lượt cho thao tác bạn bắt đầu |
| `keys.reset` | Resets in | Đặt lại sau |
| `keys.burst` | Burst | Tốc độ tức thời |
| `keys.burst.value` | {now} / {limit} per second | {now} / {limit} mỗi giây |
| `keys.egress` | Egress | Điểm gọi ra |
| `keys.lane.person` | Person lane | Luồng người dùng |
| `keys.lane.sizes` | Sizes lane | Luồng kích thước |
| `keys.history` | Last 24 hours | 24 giờ qua |
| `keys.history.legend` | red = cap reached | đỏ = đã chạm hạn mức |
| `keys.pause` | Pause | Tạm dừng |
| `keys.resume` | Resume | Tiếp tục |
| `keys.pause.aria` | Pause {label} | Tạm dừng {label} |
| `keys.resume.aria` | Resume {label} | Tiếp tục {label} |
| `keys.remove` | Remove | Xóa |
| `keys.remove.aria` | Remove {label} | Xóa {label} |
| `keys.accounts.title` | Flickr accounts connected | Tài khoản Flickr đã kết nối |
| `keys.accounts.none` | No Flickr account is connected to this key. | Chưa có tài khoản Flickr nào kết nối với khóa này. |
| `keys.accounts.connect` | Connect an account | Kết nối tài khoản |
| `keys.accounts.reconnect` | Reconnect | Kết nối lại |
| `keys.accounts.ok` | Connected | Đã kết nối |
| `keys.accounts.reconnect-required` | Reconnect needed | Cần kết nối lại |
| `keys.accounts.revoked` | Access revoked | Đã thu hồi quyền |
| `keys.accounts.error` | Accounts could not load. | Không tải được tài khoản. |
| `keys.reserve.hint` | In reserve: only actions you start use this key now. | Đang ở mức dự trữ: chỉ thao tác bạn bắt đầu dùng khóa này. |
| `keys.exhausted.hint` | Cap reached. Crawling resumes at {time}. | Đã chạm hạn mức. Thu thập tiếp tục lúc {time}. |
| `keys.paused.hint` | Paused: no scheduled calls. Resume to crawl again. | Đã tạm dừng: không có lượt gọi theo lịch. Hãy tiếp tục để thu thập lại. |
| `keys.revoked.hint` | Flickr no longer accepts this key. Add a new key to keep crawling. | Flickr không còn chấp nhận khóa này. Hãy thêm khóa mới để tiếp tục thu thập. |
| `keys.note` | More keys or more nodes do not raise the limit of one key. Each key always calls Flickr from one node so that a block on one IP does not touch your other keys. | Thêm khóa hay thêm nút không làm tăng hạn mức của một khóa. Mỗi khóa luôn gọi Flickr từ một nút để việc chặn một IP không ảnh hưởng các khóa khác của bạn. |
| `keys.empty.title` | No API key yet | Chưa có khóa API |
| `keys.empty.text` | Add your Flickr API key and secret to start crawling. | Hãy thêm khóa API và mã bí mật Flickr để bắt đầu thu thập. |
| `keys.empty.cta` | Add API key | Thêm khóa API |
| `keys.add.title` | Add API key | Thêm khóa API |
| `keys.add.label` | Label | Nhãn |
| `keys.add.label-placeholder` | Personal key | Khóa cá nhân |
| `keys.add.key` | API key | Khóa API |
| `keys.add.key-placeholder` | 32 hex characters | 32 ký tự hex |
| `keys.add.secret` | Secret | Mã bí mật |
| `keys.add.secret-placeholder` | 16 hex characters | 16 ký tự hex |
| `keys.add.secret-show` | Show secret | Hiện mã bí mật |
| `keys.add.secret-hide` | Hide secret | Ẩn mã bí mật |
| `keys.add.help` | XFlickr checks the key with one flickr.test.echo call and places it on the least loaded healthy node. The secret is stored encrypted and never shown again. | XFlickr kiểm tra khóa bằng một lượt gọi flickr.test.echo và đặt nó trên nút khỏe, ít tải nhất. Mã bí mật được lưu mã hóa và không hiện lại. |
| `keys.add.submit` | Add key | Thêm khóa |
| `keys.add.submitting` | Checking key… | Đang kiểm tra khóa… |
| `keys.remove.title` | Remove this key? | Xóa khóa này? |
| `keys.remove.text` | {label} and the {count} Flickr accounts connected through it are removed. Queued work for this key stops. This cannot be undone. | {label} và {count} tài khoản Flickr kết nối qua khóa này sẽ bị xóa. Công việc đang chờ của khóa này sẽ dừng. Không thể hoàn tác. |
| `keys.remove.confirm` | Remove key | Xóa khóa |
| `keys.toast.added` | Key checked and placed on {node} | Đã kiểm tra khóa và đặt trên {node} |
| `keys.toast.paused` | {label} paused | Đã tạm dừng {label} |
| `keys.toast.resumed` | {label} resumed | Đã tiếp tục {label} |
| `keys.toast.removed` | Key removed | Đã xóa khóa |
| `keys.error.title` | Keys could not load | Không tải được khóa |
| `keys.error.text` | Check your connection and try again. Your keys are safe. | Hãy kiểm tra kết nối và thử lại. Các khóa của bạn vẫn an toàn. |
| `keys.error.label-required` | Enter a label (up to 80 characters). | Hãy nhập nhãn (tối đa 80 ký tự). |
| `keys.error.key-format` | The key should be 32 characters, 0-9 and a-f. | Khóa phải gồm 32 ký tự, 0-9 và a-f. |
| `keys.error.secret-format` | The secret should be 16 characters, 0-9 and a-f. | Mã bí mật phải gồm 16 ký tự, 0-9 và a-f. |
| `keys.error.key-invalid` | Flickr did not accept this key and secret. Copy them again from your Flickr app page. | Flickr không chấp nhận khóa và mã bí mật này. Hãy sao chép lại từ trang ứng dụng Flickr của bạn. |
| `keys.error.key-duplicate` | You already added this key. | Bạn đã thêm khóa này rồi. |
| `keys.error.no-node` | No healthy node can take a new key right now. Try again in a few minutes. | Hiện chưa có nút khỏe nào nhận khóa mới. Hãy thử lại sau vài phút. |
| `keys.error.add-failed` | We could not add the key. Your input is kept. Try again. | Không thêm được khóa. Nội dung bạn nhập vẫn được giữ. Hãy thử lại. |
| `keys.error.action-failed` | That did not work. Nothing changed. | Thao tác không thành công. Chưa có gì thay đổi. |
| `keys.error.reconnect-failed` | Could not start the Flickr connection. Try again. | Không bắt đầu được kết nối Flickr. Hãy thử lại. |
| `keys.stale.usage` | Usage numbers are out of date (last update {time}). | Số liệu sử dụng chưa mới (cập nhật lúc {time}). |
| `keys.forbidden.title` | You cannot view keys | Bạn không thể xem khóa |
| `keys.forbidden.text` | Your account does not have access to this page. Ask the administrator. | Tài khoản của bạn không có quyền vào trang này. Hãy hỏi quản trị viên. |
| `keys.forbidden.cta` | Back to Home | Về Trang chủ |
| `keys.live.state-changed` | {label} is now {state}. | {label} hiện ở trạng thái {state}. |
| `common.retry` | Try again | Thử lại |
| `common.cancel` | Cancel | Hủy |

## Test IDs

Each name is a `data-testid` value, following patterns.md
(`keys-<area>-<element>`). A key id or account nsid is the stable suffix. Tests select by role first and by test id when text depends
on language.

- Page: `keys-page`, `keys-title`, `keys-lead`, `keys-add`, `keys-grid`,
  `keys-note`, `keys-skeleton`.
- Card: `keys-card-<id>`, `keys-card-label`, `keys-card-masked`,
  `keys-card-state`, `keys-card-paused`, `keys-card-used`, `keys-card-bar`,
  `keys-card-reserve`, `keys-card-reset`, `keys-card-burst`, `keys-card-node`,
  `keys-card-lane-person`, `keys-card-lane-sizes`, `keys-card-history`,
  `keys-card-hint`, `keys-card-pause`, `keys-card-remove`.
- Accounts: `keys-accounts-<id>`, `keys-account-<nsid>`,
  `keys-account-status-<nsid>`, `keys-account-reconnect-<nsid>`,
  `keys-accounts-none`, `keys-accounts-connect-<id>`, `keys-accounts-retry`.
- Add modal: `keys-add-modal`, `keys-add-label`, `keys-add-key`,
  `keys-add-secret`, `keys-add-secret-toggle`, `keys-add-submit`,
  `keys-add-cancel`, `keys-add-error`, `keys-add-error-label`,
  `keys-add-error-key`, `keys-add-error-secret`.
- Remove modal: `keys-remove-modal`, `keys-remove-confirm`,
  `keys-remove-cancel`.
- States: `keys-empty`, `keys-empty-cta`, `keys-error`, `keys-error-retry`,
  `keys-stale-banner`, `keys-stale-retry`, `keys-forbidden`,
  `keys-forbidden-cta`, `keys-live-region`.

## Test hooks

The list above is the contract for `web/e2e/user-keys.spec.ts`. Minimum
journeys: empty state then Add key; invalid hex, `key_invalid` and
`key_duplicate` errors keep the modal open; Pause then Resume; Remove with
confirm and cancel; a mixed page with `ok`, `reserve`, `exhausted` and
`revoked` cards; account Reconnect starts the connect call; a live `key.quota`
event updates the numbers; polling fallback when SSE is blocked; stale banner
when usage fails; list error with retry; 403 forbidden; EN/VI switch; 390 px
layout without horizontal scroll.

## Out of scope

- Editing limits, rotating a secret or renaming a key (the API allows it;
  a later ticket specifies the edit form). Fleet-wide quota and moving a key
  between nodes (`admin-quota`, `admin-nodes`).
- The Flickr accounts list and OAuth callback screen (`user-accounts`,
  `user-account-connect`); Reconnect here only starts the flow.
- The shell banner for a revoked key and the notification text (user-shell).
- OpenAPI edits (only requested above) and application code.
