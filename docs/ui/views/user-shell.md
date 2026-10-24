# User app shell, navigation and global states

View id `user-shell`, role USER, light gallery theme (dark optional). The shell
wraps every USER route. Baseline: `topbar()` and `previewWrap()` in the
[POC](../poc/xflickr-poc.html). Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md). Mockup:
[user-shell.html](../mockups/user-shell.html). Build tickets: JOOXF-79,
JOOXF-166.

## Purpose

The shell gives a signed-in user one stable frame around every view: where they
are (tabs, breadcrumbs), what is happening (Live indicator, download tray,
notifications) and what is wrong globally (expired session, lost connection,
revoked key, expired token, maintenance, 403, 404). It owns no photo data.
Views render inside `<main id="main">`.

## Route and access

- Applies to every USER route: `/`, `/library`, `/search`, `/contacts`,
  `/contacts/:id`, `/albums`, `/albums/:id`, `/crawl`, `/keys`, `/downloads`,
  `/sync`, `/sharing`, `/profile`, `/accounts`, `/sessions`. `/onboarding`
  uses the shell without the tab row (first run, nothing to navigate to).
- Access: a valid `xf_session` cookie. `GET /me` runs once on boot.
  - 401: session-expired modal (see Global states) and `return_to`.
  - `role = admin`: redirect to `/admin` (the ADMIN shell is another ticket).
    A user opening `/admin/*` gets the 403 page, never the admin shell.
  - Unknown route or id: 404 page inside the shell.
- Auth pages (`/login`, `/register`, `/setup`) do not use the shell.
- Theme: `data-role="user"`; `data-theme` is the stored choice, else the system
  preference (design-system). `<html lang>` follows `GET /me` `locale`.

## Layout

### Top bar (sticky, surface background, 1 px bottom line)

Row 1, left to right, 12 px gap, 16 px side padding (safe-area aware):

1. Brand: two dots and the word XFlickr, links to `/`.
2. Search box: wide (flex 1, max 640 px, centred), pill, 42 px high, `/`
   focuses it, Enter goes to `/search?q=<text>`. Placeholder
   `shell.search.placeholder`.
3. Live/Paused toggle (`txt-btn`, dot plus word, `aria-pressed`).
4. Language toggle `EN` / `VI` (`txt-btn`).
5. Theme toggle (icon button: moon in light, sun in dark).
6. Download tray button: tray icon and count badge. Hidden count when 0.
7. Notification bell with unread count badge (`99+` above 99).
8. Avatar menu button (30 px avatar).

Row 2: tab row, 11 tabs in this order: Home `/`, Library `/library`, Search
`/search`, Contacts `/contacts`, Albums `/albums`, Crawl `/crawl`, Keys & quota
`/keys`, Downloads `/downloads`, Cloud sync `/sync`, Sharing `/sharing`,
Profile `/profile`. The active tab has `aria-current="page"` and a 2.5 px
accent underline. Contact and Album pages keep Contacts or Albums active.
Downloads shows a count of running jobs. The row scrolls sideways when it does
not fit, with no visible scrollbar and the active tab scrolled into view.

### Main area

`<main>`: 22 px top padding, 90 px bottom padding, max width 1440 px. Order
inside main (top to bottom): banners (offline, key revoked, token expired),
breadcrumb, view content. Toasts stack bottom centre, 64 px above the bottom.

### Breadcrumbs

Only on Contact and Album pages, above the page title: a single back link
`‹ Contacts` (`/contacts`) or `‹ Albums` (`/albums`) as in the POC `crumb()`.
Decision (not in the POC): on an album opened from a contact page the crumb is
`‹ <contact name>` back to `/contacts/:id`; the source is `history.state.from`,
falling back to `‹ Albums`. A full trail is not used, so mobile never wraps.

### Download tray

The tray button opens a popover (drawer from the bottom on 390 px): list of
photos collected for download with thumbnail, title, owner and a remove
button; footer shows `shell.tray.summary` (count, estimated size) and the
primary `Download` button (opens the new-download modal with scope tray) and
`Clear tray`. Maximum 500
photos; adding the 501st shows toast `shell.tray.full`. Items are added from
photo tiles and the lightbox (`+` button, POC `tray-btn`). The count badge
updates at once.

### Avatar menu

Menu items in order: user name and email (not interactive), Profile
(`/profile`), Flickr accounts (`/accounts`), Sessions (`/sessions`), Keyboard
shortcuts (opens help dialog), Sign out. Esc closes; focus returns to the
button.

## Data

Short form: the shell reads `GET /me`, `GET /shell/summary`, the notification
list and the SSE stream. Fields, endpoints and contract requests are in
[Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Identity, avatar, locale | `GET /me` (exists) | `id`, `email`, `role`, `locale` |
| Display name, theme | `GET /me` **contract request** | `display_name` (string, may be empty), `theme` (`light`,`dark`,`system`) |
| Badges and banners | `GET /shell/summary` **contract request** | `unread_notifications` int, `running_downloads` int, `tray_count` int, `attention[]` of `{kind: key_revoked or token_expired or cloud_expired, id, label}` |
| Notification list | `GET /notifications?limit=30&cursor=` **contract request** | `id`, `kind`, `tone` (`ok`,`warn`,`bad`,`info`), `text_key`, `params`, `created_at`, `read` |
| Mark read | `POST /notifications/read` **contract request** | body `{ids[]}` or `{all:true}`; 204 |
| Tray | `GET /tray`, `PUT /tray/items/{photo_id}`, `DELETE /tray/items/{photo_id}`, `DELETE /tray` **contract request** | `items[]` `{photo_id, title, owner_name, thumb_url, added_at}`, `count`, `estimated_bytes` |
| Save language and theme | `PATCH /me` **contract request** | body `{locale?, theme?}`; 200 `User` |
| Keys and accounts status | `GET /keys` (exists), `GET /flickr-accounts` **contract request** | key `status` (`valid`,`invalid`,`revoked`); account `status` (`ok`,`reconnect_required`,`revoked`), `nsid`, `username` |
| Live channel | `GET /api/v1/events/stream` (SSE) | channels `notifications.me`, `downloads.jobs`, `quota.keys` (patterns) |
| Maintenance probe | `GET /healthz` (exists) | HTTP status and `status` |
| Sign out | `POST /auth/logout` (exists) | 204 |

Contract requests are only requested here; OpenAPI is changed by the build
tickets. Tray is stored on the server (not only in memory as in the POC) so it
survives reload and shows on a second device. Search suggestions are not
requested: the search box has no autocomplete in this release.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Go to a tab | click, Enter | Router push, focus moves to the page `h1`, document title is `<view> · XFlickr` |
| Search | submit box | `/search?q=`; empty text only focuses the box |
| Toggle Live | click, `l` | Pause or resume live (patterns). Toast `shell.live.paused-toast` or `shell.live.resumed-toast` |
| Toggle language | click | Switch EN/VI at once, `PATCH /me {locale}`, update `<html lang>` |
| Toggle theme | click | Switch light/dark at once, `PATCH /me {theme}` |
| Open tray | click | Popover with `GET /tray` |
| Download from tray | button | Opens the new-download modal (scope tray). The tray is cleared when the job is created |
| Remove / clear tray | button | `DELETE` item or tray; toast with `shell.tray.undo` (8 s) |
| Open notifications | click bell | Drawer (POC `vNotifs`), lists `GET /notifications`; opening does not mark read |
| Mark all read | button in drawer | `POST /notifications/read {all:true}`, badge clears |
| Open avatar menu | click | Menu (above) |
| Reconnect (banner) | button | Flickr account: `POST /flickr-accounts/connect`, then redirect to Flickr. Cloud provider: go to `/sync` |
| Retry (error, offline) | button | Repeat `GET /me` and `GET /shell/summary` |
| Sign in again | modal button | `/login?return_to=<path>` |
| Sign out | menu | `POST /auth/logout`, go to `/login` |

## Interactions

- Keyboard: `/` search, `l` Live, `g` then `h` home, `?` shortcuts (a11y.md).
- Hover is never the only way: tray `+` on photo tiles is visible on touch.
- Every route change closes menus, popovers and drawers, scrolls to top, and
  moves focus to the page heading.
- Preview of Normal, Loading, Empty and Error (POC `.proto` control) exists in
  development builds only, behind `import.meta.env.DEV`. It is not shipped.

## States

State scope: the shell chrome and global surfaces. Each view specifies its own
content states inside `<main>`.

### Ready

Normal: all controls shown as in Layout. The Live dot is green when connected.

### Loading

Boot, while `GET /me` and `GET /shell/summary` run (skeleton after 200 ms,
kept at least 400 ms, error after 10 s): the top bar shows the real brand and
the real tab labels (static), skeleton blocks in place of search value,
badges and avatar, the line `Loading XFlickr…` (`role="status"`) and a tile
skeleton in main. Tabs are not clickable until `/me` answers. Refreshing the
summary never shows a skeleton.

### Empty

A new user with no Flickr account and nothing collected. The shell is complete;
tray popover shows the empty block `shell.tray.empty.*`, the notification drawer
shows `shell.notifications.empty.*`, and main shows the Home empty block (copy
and CTA from patterns: "Nothing crawled yet", CTA `Connect account`). Badges
are hidden at 0.

### Error

`GET /me` fails with a network error, 5xx or timeout: the top bar renders with
disabled controls, main shows a `banner bad` `shell.error.title` /
`shell.error.text` with `Retry`, the skeleton below at 35 % opacity. Failure of
`/shell/summary` alone is partial: badges are hidden, a `Stale` pill is not
needed, and a toast is not shown; the next poll retries.

### Partial and stale

- Tray or notification list failed: the popover or drawer shows an inline
  compact error (`shell.tray.error`, `shell.notifications.error`) with `Retry`; the rest works.
- Live channel down: indicator `Paused`, title with the reason, `shell.stale` marker
  with `shell.stale.age` on badges (title attribute plus drawer header text).

### Forbidden

403 page inside the shell: lock icon, `shell.forbidden.*`, CTA `shell.forbidden.home`.
Document title `Access denied`. Does not reveal whether the resource exists.

### Not found

404 page inside the shell: search icon, `shell.notfound.*`, CTA `Go to home`
and secondary `Back`. Document title `Page not found`. A private item of
another user also shows this page.

### Global states

| State | Trigger | Surface and behaviour |
| --- | --- | --- |
| Session expired | any API call returns 401 | All polling and SSE stop. Modal `shell.session.*` (focus inside, assertive announce), button `Sign in` goes to `/login?return_to=<current path + query>`. `return_to` is a same-origin path only. Unsaved form input is kept in memory and restored after sign-in; never auto-submitted. A 403 `csrf_rejected` refreshes the CSRF token once instead |
| Maintenance | `GET /healthz` is not 200, or API answers 503 with code `database_unavailable`, 3 times in a row | Full page without tabs: wrench icon, `shell.maintenance.*`, auto retry every 15 s with backoff to 60 s, `Retry now` button, status text "Last checked HH:mm". SSE and writes stop. Returns to the previous route when healthy |
| Offline | `navigator.onLine` false or network error | Persistent info banner `shell.offline.banner`, no dismiss. Live shows `Paused` with title `shell.offline.title`. Writes disabled with title `shell.offline.needs-connection`. SSE falls back to polling per patterns (3 failed reconnects, backoff 1 s to 15 s, jitter 20 %), polling 2 s / 5 s / 10 s by data type. On return: toast `shell.offline.back`, one refetch, banner clears |
| SSE to polling | SSE blocked, error or 3 failed reconnects while online | No banner. Indicator stays `Live` while polling works; the title reads "Polling every 10 s". If polling also fails, state becomes Offline-like with `Paused` and the Stale marker |
| Key revoked | summary `attention` has `key_revoked` (`GET /keys` status `revoked`) | Warning banner at the top of main on every view: `shell.banner.key-revoked` with the key label and `Reconnect`, which goes to `/keys` to add a replacement key. Dismiss hides it for the tab session; it returns on reload while true |
| Token expired | Flickr account `reconnect_required` or `revoked` (`attention` `token_expired`), or cloud `cloud_expired` | Warning banner: `shell.banner.token-expired` with the account name and `Reconnect`. Flickr: starts OAuth connect. Cloud: `/sync`. One banner per item, at most 2 shown plus "N more" link to `/accounts` |

Banner order in main: offline, key revoked, token expired. Maintenance and
session expired replace everything else.

## Live updates

The shell subscribes on mount to `notifications.me` (bell count and list),
`downloads.jobs` (Downloads tab count, tray after a job starts) and `quota.keys`
(key state change that raises banners). One SSE connection per tab. Fallback
polling of `GET /shell/summary` every 10 s (2 s while a download is running)
with backoff 10, 20, 40, 60 s; polling pauses when the tab is hidden and
fetches once on show. A badge change updates in place with no animation. A new
notification does not toast (patterns); it only raises the count. Paused by the
user buffers events (max 200); resume refetches the summary.

## Responsive

| Width | Behaviour |
| --- | --- |
| ≥ 1101 | Everything on one row 1; tab row below, all tabs visible |
| ≤ 900 | Tab row scrolls sideways; search box narrows to min 220 px |
| ≤ 720 | Top bar wraps: row 1 brand and actions; search takes its own full-width row (40 px); Live shows only the dot with the word hidden visually (still in the accessible name); `hide-sm` hides the avatar button, whose menu moves to the Profile tab and a "Sign out" button at the page foot of Profile; tray opens as a bottom sheet; notifications drawer is full width; banners stack above content, buttons wrap under text |
| 390 | Targets are at least 40 px; the toast stack stays inside safe areas; maintenance and 403/404 pages are one column |

Touch devices see the tray `+` on tiles without hover. No horizontal page
scroll at 320 CSS px or 200 % zoom.

## Accessibility

- Landmarks: `<header>` with `<nav aria-label>` for tabs, `role="search"` on
  the search form, `<main id="main">`. First element is a skip link
  `shell.skip` to `#main`.
- Tabs are links/buttons, not ARIA `tab`; the current one has
  `aria-current="page"`. Keyboard: Tab order is brand, search, toggles, tray,
  bell, avatar, then tabs.
- Toggles: Live and theme use `aria-pressed`; language toggle names the target
  language (`aria-label` "Switch to Tiếng Việt").
- Badges have text alternatives: bell "Notifications, 3 unread"; tray
  "Download tray, 12 photos".
- Menu, popover and drawer: focus moves in, is trapped for drawer and sheet,
  Esc closes and returns focus to the opener.
- Session modal uses `role="alertdialog"`; offline banner `role="status"`;
  key/token banners `role="alert"` on first render only.
- One polite region announces Live changes, `shell.a11y.offline` and `shell.a11y.online`
  (a11y.md). Colour is never the only signal: states have a word.
- Contrast and motion follow the design system; skeleton shimmer is static
  under reduced motion.

## i18n

Keys follow `area.view.element`. Interpolation: `{count}`, `{name}`, `{label}`,
`{time}`. Counts use ICU plural with `other`. Names, titles and tags from
Flickr are not translated. Nav labels reuse the POC glossary.

| Key | EN | VI |
| --- | --- | --- |
| `shell.brand` | XFlickr | XFlickr |
| `shell.skip` | Skip to content | Chuyển đến nội dung |
| `shell.nav.label` | Sections | Các mục |
| `shell.nav.home` | Home | Trang chủ |
| `shell.nav.library` | Library | Thư viện |
| `shell.nav.search` | Search | Tìm kiếm |
| `shell.nav.contacts` | Contacts | Liên hệ |
| `shell.nav.albums` | Albums | Album |
| `shell.nav.crawl` | Crawl | Thu thập |
| `shell.nav.keys` | Keys & quota | Khóa & hạn mức |
| `shell.nav.downloads` | Downloads | Tải xuống |
| `shell.nav.sync` | Cloud sync | Đồng bộ đám mây |
| `shell.nav.sharing` | Sharing | Chia sẻ |
| `shell.nav.profile` | Profile | Hồ sơ |
| `shell.search.placeholder` | Search your Flickr circle, e.g. trees, a person and a dog | Tìm trong vòng Flickr của bạn, ví dụ: cây, một người và một con chó |
| `shell.search.label` | Search | Tìm kiếm |
| `shell.live.on` | Live | Trực tiếp |
| `shell.live.paused` | Paused | Tạm dừng |
| `shell.live.paused-toast` | Live data paused | Đã tạm dừng dữ liệu trực tiếp |
| `shell.live.resumed-toast` | Live data resumed | Đã tiếp tục dữ liệu trực tiếp |
| `shell.live.reason` | Paused: {reason} | Tạm dừng: {reason} |
| `shell.stale` | Stale | Chưa mới |
| `shell.stale.age` | Last updated {time} ago | Cập nhật lần cuối {time} trước |
| `shell.live.polling` | Polling every {seconds} s | Đang cập nhật mỗi {seconds} giây |
| `shell.lang.switch` | Switch to Tiếng Việt | Chuyển sang English |
| `shell.theme.toggle` | Switch between light and dark | Chuyển giữa sáng và tối |
| `shell.notifications.button` | {count, plural, =0 {Notifications} one {Notifications, # unread} other {Notifications, # unread}} | {count, plural, =0 {Thông báo} other {Thông báo, # chưa đọc}} |
| `shell.notifications.title` | Notifications | Thông báo |
| `shell.notifications.mark-all` | Mark all read | Đánh dấu đã đọc tất cả |
| `shell.notifications.error` | Could not load notifications. | Không thể tải thông báo. |
| `shell.notifications.empty.title` | No notifications | Chưa có thông báo |
| `shell.notifications.empty.text` | Updates about your downloads, keys and accounts show up here. | Cập nhật về tải xuống, khóa và tài khoản của bạn sẽ hiện ở đây. |
| `shell.tray.button` | {count, plural, =0 {Download tray} one {Download tray, # photo} other {Download tray, # photos}} | {count, plural, =0 {Khay tải xuống} other {Khay tải xuống, # ảnh}} |
| `shell.tray.title` | Download tray | Khay tải xuống |
| `shell.tray.download` | Download | Tải xuống |
| `shell.tray.clear` | Clear tray | Xóa khay |
| `shell.tray.undo` | Undo | Hoàn tác |
| `shell.tray.summary` | {count, plural, one {# photo} other {# photos}} · about {size} | {count, plural, other {# ảnh}} · khoảng {size} |
| `shell.tray.error` | Could not load the tray. | Không thể tải khay. |
| `shell.tray.remove` | Remove from tray | Bỏ khỏi khay |
| `shell.tray.removed-toast` | Removed from tray | Đã bỏ khỏi khay |
| `shell.tray.full` | The tray holds up to 500 photos | Khay chứa tối đa 500 ảnh |
| `shell.tray.empty.title` | Your tray is empty | Khay của bạn đang trống |
| `shell.tray.empty.text` | Use the plus button on a photo to collect it for download. | Dùng nút dấu cộng trên ảnh để gom ảnh cần tải. |
| `shell.avatar.button` | Account menu | Menu tài khoản |
| `shell.avatar.header` | Signed in as {name} ({email}) | Đăng nhập với {name} ({email}) |
| `shell.avatar.profile` | Profile | Hồ sơ |
| `shell.avatar.accounts` | Flickr accounts | Tài khoản Flickr |
| `shell.avatar.sessions` | Sessions | Phiên đăng nhập |
| `shell.avatar.shortcuts` | Keyboard shortcuts | Phím tắt |
| `shell.avatar.sign-out` | Sign out | Đăng xuất |
| `shell.crumb.back-to` | Back to {name} | Quay lại {name} |
| `shell.loading` | Loading XFlickr… | Đang tải XFlickr… |
| `shell.error.title` | Could not load your workspace. | Không thể tải không gian làm việc của bạn. |
| `shell.error.text` | The server did not answer within 10 s. Check your connection and try again. | Máy chủ không phản hồi trong 10 giây. Hãy kiểm tra kết nối và thử lại. |
| `shell.error.retry` | Retry | Thử lại |
| `shell.home.empty.title` | Nothing crawled yet | Chưa thu thập gì |
| `shell.home.empty.text` | Connect a Flickr account to start. Your contacts appear first, then their photos. | Hãy kết nối tài khoản Flickr để bắt đầu. Liên hệ của bạn hiện trước, sau đó là ảnh của họ. |
| `shell.home.empty.cta` | Connect account | Kết nối tài khoản |
| `shell.forbidden.title` | You do not have access to this page | Bạn không có quyền truy cập trang này |
| `shell.forbidden.text` | This page is for administrators. Your account has the User role. | Trang này dành cho quản trị viên. Tài khoản của bạn có vai trò Người dùng. |
| `shell.forbidden.home` | Go to home | Về trang chủ |
| `shell.notfound.title` | We could not find that page | Chúng tôi không tìm thấy trang đó |
| `shell.notfound.text` | The link may be old, or the item was removed. | Liên kết có thể đã cũ, hoặc mục này đã bị xóa. |
| `shell.notfound.home` | Go to home | Về trang chủ |
| `shell.notfound.back` | Back | Quay lại |
| `shell.session.title` | Your session ended | Phiên đăng nhập đã kết thúc |
| `shell.session.text` | Sign in again to continue. Your place is saved. | Hãy đăng nhập lại để tiếp tục. Vị trí của bạn đã được lưu. |
| `shell.session.sign-in` | Sign in | Đăng nhập |
| `shell.maintenance.title` | XFlickr is under maintenance | XFlickr đang bảo trì |
| `shell.maintenance.text` | The database is not reachable. Your data is safe. We retry automatically. | Không kết nối được cơ sở dữ liệu. Dữ liệu của bạn an toàn. Hệ thống tự thử lại. |
| `shell.maintenance.checked` | Last checked {time} | Kiểm tra lần cuối {time} |
| `shell.maintenance.retry` | Retry now | Thử lại ngay |
| `shell.offline.banner` | You are offline. Showing the last data we have. | Bạn đang ngoại tuyến. Đang hiển thị dữ liệu gần nhất. |
| `shell.offline.title` | Offline | Ngoại tuyến |
| `shell.a11y.offline` | You are offline | Bạn đang ngoại tuyến |
| `shell.a11y.online` | Back online | Đã kết nối lại |
| `shell.offline.needs-connection` | Needs a connection | Cần có kết nối |
| `shell.offline.back` | Back online | Đã kết nối lại |
| `shell.banner.key-revoked` | Flickr revoked the API key {label}. Add a new key to keep crawling. | Flickr đã thu hồi khóa API {label}. Hãy thêm khóa mới để tiếp tục thu thập. |
| `shell.banner.token-expired` | The Flickr sign-in for {name} expired. Reconnect to resume crawling. | Phiên Flickr của {name} đã hết hạn. Hãy kết nối lại để tiếp tục thu thập. |
| `shell.banner.cloud-expired` | The {name} sign-in expired. Uploads are paused. | Phiên {name} đã hết hạn. Việc tải lên đang tạm dừng. |
| `shell.banner.reconnect` | Reconnect | Kết nối lại |
| `shell.banner.more` | {count} more | Còn {count} mục nữa |
| `shell.banner.dismiss` | Dismiss | Đóng |

## Test IDs

Names follow patterns.md. Repeated items use the stable id as suffix.

- Chrome: `shell-topbar`, `shell-skip-link`, `shell-brand`, `shell-search`,
  `shell-search-input`, `shell-live-toggle`, `shell-lang-toggle`,
  `shell-theme-toggle`, `shell-tray-button`, `shell-tray-count`,
  `shell-notifications-button`, `shell-notifications-count`,
  `shell-avatar-button`, `shell-main`.
- Tabs: `shell-nav`, `shell-nav-home`, `shell-nav-library`, `shell-nav-search`,
  `shell-nav-contacts`, `shell-nav-albums`, `shell-nav-crawl`, `shell-nav-keys`,
  `shell-nav-downloads`, `shell-nav-sync`, `shell-nav-sharing`,
  `shell-nav-profile`, `shell-nav-downloads-count`.
- Breadcrumb: `shell-crumb`.
- Tray: `shell-tray`, `shell-tray-item-<photoId>`,
  `shell-tray-item-<photoId>-remove`, `shell-tray-download`,
  `shell-tray-clear`, `shell-tray-empty`, `shell-tray-error`,
  `shell-tray-summary`, `shell-tray-undo`.
- Notifications: `shell-notifications`, `shell-notifications-item-<id>`,
  `shell-notifications-mark-all`, `shell-notifications-empty`,
  `shell-notifications-error`.
- Avatar menu: `shell-avatar-menu`, `shell-avatar-header`, `shell-avatar-profile`,
  `shell-avatar-accounts`, `shell-avatar-sessions`, `shell-avatar-shortcuts`,
  `shell-avatar-sign-out`.
- States: `shell-skeleton`, `shell-empty`, `shell-error`, `shell-error-retry`,
  `shell-stale`, `shell-toast`.
- Global: `shell-offline-banner`, `shell-key-revoked-banner-<keyId>`,
  `shell-token-expired-banner-<accountId>`, `shell-banner-reconnect-<id>`,
  `shell-banner-dismiss-<id>`, `shell-session-modal`,
  `shell-session-sign-in`, `shell-maintenance`, `shell-maintenance-retry`,
  `shell-forbidden`, `shell-forbidden-home`, `shell-notfound`,
  `shell-notfound-home`, `shell-notfound-back`.

## Test hooks

The `data-testid` list above is the contract for `web/e2e/user-shell.spec.ts`.
Tests select by role, or by test id when text depends on language. The dev-only
preview control `shell-preview-<state>` is absent from production builds.

## Out of scope

- The ADMIN shell (left rail, admin tabs) and the admin overview.
- Content of each view inside `<main>`; the lightbox and new-download modal
  internals; the Profile settings form; sign-in and registration pages.
- OpenAPI edits (only requested above), application code, push notifications
  and email.
