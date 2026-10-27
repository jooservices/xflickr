# Notifications panel

View id `user-notifications`, USER role, light gallery theme (dark optional).
The panel is a shell drawer opened from the bell on any USER route; it has no
standalone page route. Baseline: `vNotifs()`, `NOTIFS`, and the bell button in
the approved [POC](../poc/xflickr-poc.html). Follow the [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), and [i18n](../i18n.md). Mockup:
[user-notifications.html](../mockups/user-notifications.html). Build ticket:
JOOXF-168.

## Purpose

Give a signed-in user a current, actionable history of account and library
events. The bell and drawer are available throughout the USER shell. The
panel contains at most 50 newest notifications at a time and does not replace
the dedicated Downloads, Sync, Keys, or Contacts views.

## Route and access

- Shell overlay on every authenticated USER route, opened by the top bar bell.
  It is not addressable as a standalone route. Close with the close button,
  Escape, or the scrim; restore focus to the bell.
- Authenticated session only; the server scopes every request to the session.
  No user id is sent by the client. ADMIN uses the same panel for their own
  user data. A 401 follows the shell session-expiry flow.
- Opening the drawer loads the first page. The bell badge shows the unread
  count, capped visually at `99+`.

## Layout

Use the USER light gallery theme by default and optional USER dark theme. The
POC uses a narrow 420px right drawer on desktop, a sticky header with title,
Mark all read and Close, and a scrollable list of rounded rows. Each row has a
type icon, readable message, relative time, unread dot and full-row deep link.
Use the existing drawer, list row, icon, button, skeleton and empty-block
components. Do not add a component.

Notification icons use the POC line icon family: quota `alert`, download ready
`download`, sync failed `cloud`, key revoked `key`, and new contact `user`.
Include a text label/message so icon, tone and unread status never carry
meaning alone. Fresh rows animate from accent-soft to the resting surface.

## Data and API

Paths are relative to `/api/v1`. The existing OpenAPI contract has no
notification endpoints or notification SSE events. The following are contract
requests for JOOXF-168; do not call uncontracted endpoints before that contract
lands. The shared `Error` envelope is `{code, message, details}`; localize by
`code` and never render server `message`.

| Content | Contract request |
| --- | --- |
| List | `GET /notifications?limit=50&cursor={cursor}` returns `items[]`, `unread_count`, `next_cursor`, `server_time`. Each item is `{id, type, created_at, read_at, data}`. `type` is `quota`, `download_ready`, `sync_failed`, `key_revoked`, or `new_contact`. `read_at` is null when unread. |
| Mark one read | `PATCH /notifications/{id}` with `{read: true}`; return updated notification and `unread_count`. Ids outside the current user scope return the same not-found response as unknown ids. |
| Mark all read | `POST /notifications/read-all`; return `{unread_count: 0, read_at}`. |
| Live arrival | Extend authenticated `GET /events` with user-scoped `notification.created`, carrying `{notification: {id, type, created_at, read_at, data}}`. Event frames keep the existing id and `Last-Event-ID` resume behavior. Notification SSE is a contract request. |

`data` is a type-specific object. It contains only display-safe fields; never
include API key secrets, OAuth tokens, or private photo content.

| Type and icon | Data source | Row content | Row action |
| --- | --- | --- | --- |
| `quota` · `alert` | `GET /notifications`, `items[].data.key_id, key_label, used, limit, reset_at` | “{key_label} reached {limit} calls.” | Open `/keys?key={key_id}`; if `key_id` is absent, open `/keys`. |
| `download_ready` · `download` | `GET /notifications`, `items[].data.download_id, source_label, expires_at` | “ZIP for {source_label} is ready.” | Open `/downloads?download={download_id}`; if `download_id` is absent, open `/downloads`. |
| `sync_failed` · `cloud` | `GET /notifications`, `items[].data.upload_id, provider_name, error_code` | “{provider_name} sync needs attention.” | Open `/sync?upload={upload_id}`; if `upload_id` is absent, open `/sync`. |
| `key_revoked` · `key` | `GET /notifications`, `items[].data.key_id, key_label` | “{key_label} was revoked.” | Open `/keys?key={key_id}`; if `key_id` is absent, open `/keys`. |
| `new_contact` · `user` | `GET /notifications`, `items[].data.count, depth` | “{count} new contacts were discovered at depth {depth}.” | Open `/contacts?sort=recent`. |

The list is newest first by `created_at`; stable ties use descending `id`.
Render relative time from `created_at` and synchronize age with `server_time`.
Unknown future types are kept in the list as a generic informational row with
no deep link. Invalid or absent destination ids fall back to the type's
collection route. Do not infer a user action from notification text.

## Actions

| Action | Effect |
| --- | --- |
| Open notifications | Open drawer, focus its heading, fetch first page and refresh bell count. |
| Open row | Navigate to the mapped destination. Mark that notification read first; navigate even if marking read fails, then show an action-failed toast. |
| Mark all read | `POST /notifications/read-all`, clear dots and badge on success. On failure preserve unread state and show retryable error feedback. |
| Load older | When `next_cursor` is present, append the next page without replacing loaded rows. |
| Close | Dismiss drawer and return focus to the bell; do not mark any rows read. |
| Retry | Retry the failed list or page request only. |

## States

### Ready

Ready (normal) state is a newest-first mixed list with the five supported type icons,
timestamps, unread dots and working deep links. Unread rows use accent-soft
background and a visible dot. Mark all read is disabled when `unread_count`
is zero. A new arrival inserts at the top and receives the fresh-row entrance
animation. Rows added while scrolled down do not move keyboard focus.

### Loading

Before the first response, show the drawer header and five row skeletons with
`aria-busy=true`; after 10 seconds show Error. While loading an older page,
keep existing rows and show one inline spinner at the end.

### Empty

When `items` is empty and there is no next cursor, show a bell illustration,
“No notifications yet”, “Updates about your key, downloads, sync and contacts
will appear here.” and a “Go to Home” CTA to `/`. The CTA closes the drawer and
navigates to Home.

### Error

If the first list request fails, show “Could not load notifications”, “Check
your connection and try again.” and Retry. A page request failure keeps loaded
rows and shows an inline retry row. A mark-read failure keeps current unread
state and shows “Could not update notifications. Try again.” Never display a
raw server message or replace already loaded data with an empty panel.

### Partial and stale

If any page fails, retain loaded rows, identify the failed section inline and
allow that page to retry. On SSE outage, mark the panel stale and preserve the
last list; successful polling clears stale. Show the last successful update
time. Polling failures do not clear notification data or unread count.

### Forbidden

401 follows the shell session-expiry flow and stops live updates. A 403 shows
the shared access-denied treatment in the drawer and stops requests. Do not
reveal whether another user's notification id exists. A 404 while marking a
row read removes that stale row after refreshing the list.

## Live updates

Subscribe to authenticated `GET /events` while the drawer is open, resume with
`Last-Event-ID`, and apply only events scoped to the signed-in user. On
`notification.created`, fetch the first list page to obtain the canonical row
and unread count, then insert it at the top. Deduplicate by notification `id`.
Do not mark a notification read from an SSE arrival.

Reconnect SSE with exponential delay (1, 2, 4, then 8 seconds, capped at 30).
After three consecutive failures, poll `GET /notifications` every 30 seconds;
stop polling while the tab is hidden and refresh immediately on visibility
return. Keep polling as fallback until SSE reconnects. A visibility refresh
also repairs missed events. Close SSE and stop polling when the drawer closes
or the session expires. Honor reduced motion; when enabled, show the final
fresh-row style without animation.

## Responsive

At viewport widths above 720px, use a 420px right drawer and dimmed scrim. At
720px and below, use a full-width bottom sheet capped at 85vh with a sticky
header and independently scrollable list. At 390px, messages and Vietnamese
copy wrap without clipping or horizontal page scrolling; keep a minimum 44px
row hit area. The drawer remains within the viewport when the virtual keyboard
opens.

## Accessibility

Use a named modal dialog with `aria-modal=true`, focus trap, Escape dismissal,
background scroll lock and focus return to the bell. The bell's accessible name
includes the localized unread count. Mark all read and Close are native buttons.
Each row is one link with a distinct accessible name including type, message,
time and unread status. Give the unread dot visually hidden “Unread” text.
Skeletons use `aria-hidden=true`; expose one `role=status` loading announcement.
Announce new arrivals politely without moving focus. Use a polite live region
for refreshed unread count and errors. Keep visible focus at 2px with 2px
offset, maintain 3:1 control/icon contrast and 4.5:1 text contrast, and honor
reduced motion. Set dialog language through the document locale.

## i18n

All user-facing strings have `notifications.*` or shared `common.*` keys. Data
values in braces are interpolated as text, never HTML. EN and VI values cover
the title, controls, all row types, loading, empty, errors, partial/stale,
forbidden and accessibility announcements.

| Key | EN | VI |
| --- | --- | --- |
| notifications.title | Notifications | Thông báo |
| notifications.close | Close notifications | Đóng thông báo |
| notifications.mark-all-read | Mark all read | Đánh dấu tất cả đã đọc |
| notifications.unread-count | {count} unread notifications | {count} thông báo chưa đọc |
| notifications.unread | Unread | Chưa đọc |
| notifications.read | Read | Đã đọc |
| notifications.time | {time} ago | {time} trước |
| notifications.type.quota | API quota | Hạn mức API |
| notifications.type.download-ready | Download ready | Tải xuống đã sẵn sàng |
| notifications.type.sync-failed | Sync failed | Đồng bộ thất bại |
| notifications.type.key-revoked | Key revoked | Khóa đã bị thu hồi |
| notifications.type.new-contact | New contacts | Liên hệ mới |
| notifications.item.quota | {key_label} used {used} of {limit} calls. Resets {reset_at}. | {key_label} đã dùng {used}/{limit} lượt gọi. Đặt lại lúc {reset_at}. |
| notifications.item.download-ready | ZIP for {source_label} is ready. Link expires {expires_at}. | ZIP của {source_label} đã sẵn sàng. Liên kết hết hạn lúc {expires_at}. |
| notifications.item.sync-failed | {provider_name} sync needs attention. | Đồng bộ {provider_name} cần được xử lý. |
| notifications.item.key-revoked | {key_label} was revoked. | {key_label} đã bị thu hồi. |
| notifications.item.new-contact | {count} new contacts were discovered at depth {depth}. | Đã tìm thấy {count} liên hệ mới ở độ sâu {depth}. |
| notifications.empty.title | No notifications yet | Chưa có thông báo |
| notifications.empty.text | Updates about your key, downloads, sync and contacts will appear here. | Cập nhật về khóa, nội dung tải xuống, đồng bộ và liên hệ sẽ xuất hiện tại đây. |
| notifications.empty.cta | Go to Home | Về trang chủ |
| notifications.loading | Loading notifications… | Đang tải thông báo… |
| notifications.error.title | Could not load notifications | Không thể tải thông báo |
| notifications.error.text | Check your connection and try again. | Kiểm tra kết nối rồi thử lại. |
| notifications.error.update | Could not update notifications. Try again. | Không thể cập nhật thông báo. Hãy thử lại. |
| notifications.error.forbidden | You do not have access to notifications. | Bạn không có quyền truy cập thông báo. |
| notifications.partial | Some notifications could not load. | Không thể tải một số thông báo. |
| notifications.stale | Updates paused · last updated {time} | Đang tạm dừng cập nhật · cập nhật lần cuối {time} |
| notifications.new-arrival | New notification: {message} | Thông báo mới: {message} |
| notifications.load-older | Load older notifications | Tải thông báo cũ hơn |
| common.retry | Retry | Thử lại |
| common.close | Close | Đóng |

## Test IDs

Stable `data-testid` E2E hooks: `notifications-bell`, `notifications-unread-count`,
`notifications-panel`, `notifications-title`, `notifications-mark-all-read`,
`notifications-list`, `notification-row-{id}`, `notification-icon-{id}`,
`notification-unread-{id}`, `notification-link-{id}`,
`notifications-load-older`, `notifications-loading`, `notifications-empty`,
`notifications-error`, `notifications-partial`, `notifications-stale`,
`notifications-forbidden`, `notifications-retry`, `notifications-close`,
`notifications-live-announcement`.

## Out of scope

Notification preferences, email or push delivery, delete/dismiss actions,
unread filtering, search, bulk selection, per-item retry, admin fleet
notifications, notification retention controls and backend/API implementation.
The contract requests above are owned by the build ticket.

## Data

See [Data and API](#data-and-api).

## Interactions

See [Actions](#actions).

## Test hooks

See [Test IDs](#test-ids).
