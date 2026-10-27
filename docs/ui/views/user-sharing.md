# Sharing

View id `user-sharing`, USER role, light gallery theme (dark optional), route
`/sharing`. Baseline: `vSharing()` and its live summary card in the approved
[POC](../poc/xflickr-poc.html). Follow the [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), and [i18n](../i18n.md).
Mockup: [user-sharing.html](../mockups/user-sharing.html). Build ticket:
JOOXF-179. The view renders in the [user shell](user-shell.md).

## Purpose

Let a signed-in user decide whether their eligible crawled public records can
be reused by other XFlickr users. Explain the reciprocal benefit and make the
privacy boundary explicit before the user changes this choice.

## Route and access

- `/sharing`; only an authenticated USER can view or change their own setting
  and aggregate counts. ADMIN access to this USER route is forbidden; fleet-wide
  sharing belongs to the separate `/admin/sharing` view.
- Unauthenticated requests follow the shell expiry flow to
  `/login?reason=expired&return_to=/sharing`. A 403 uses the shared forbidden
  view.
- The server scopes reads and writes to the session. The client sends no user
  id. Focus moves to the page `h1` after route navigation.

## Layout

Use a centered main column up to 1040px with 24px desktop gutters and 16px at
390px. Page head has title and lead explaining that sharing reduces duplicate
Flickr calls. A two-column desktop grid stacks at 720px and below.

1. **Sharing choice card:** heading “Share my crawled records”, a labeled
   switch, and a short note that only eligible public record data is shared.
   A separate lock row lists what is never shared: private, friends-only and
   family-only records, API keys/secrets, OAuth tokens, and downloads. Keep
   the complete list visible without opening a tooltip.
2. **Summary card:** heading “What sharing gives you”; three statistics: “You
   share” (the count of this user's eligible records currently visible to
   others), “You receive” (eligible public records available from other
   opted-in users), and “Calls saved” (deduplicated Flickr calls attributed
   to reuse, with period “this week”). When sharing is off, all three values
   are 0 and the off notice explains that the user sees records crawled through
   their own access.
   Include the POC warning when off: the user sees records crawled through
   their own access and their crawl may repeat calls already made by others.
3. **Turn-off confirmation:** an existing `modal` opened before sending an
   off choice. State that it takes effect as soon as confirmed and other users
   will no longer see records contributed only by this user. Buttons are
   Cancel and “Turn sharing off”. Do not show the modal when turning on.

Use existing `page-head`, `card`, `toggle/switch`, `banner`, `modal`, `stat`,
`icon`, `button`, `skeleton`, `empty-state`, `toast` components. No new
component. Match USER light gallery tokens; dark is optional and follows the
USER dark gallery token set.

## Data and API

Paths are relative to `/api/v1`. Existing endpoints are marked **exists**.
Contract requests are requirements for the build ticket; this design ticket
does not change OpenAPI.

| Content | Source and fields |
| --- | --- |
| Initial setting and summary | `GET /me/sharing` **contract request** (authenticated). Return `enabled: boolean` (false when unset), `changed_at: date-time (nullable)`, `shared_record_count: integer` (eligible records visible to others; zero when off), `received_record_count: integer` (zero when off), `calls_saved_count: integer` (zero when off), `calls_saved_period: "week"`, and `server_time: date-time`. Counts exclude private/friends/family records and are aggregate counts only. |
| Change setting | `PUT /me/sharing` **exists**. Send `{enabled: boolean}`; response has `enabled`, `changed_at`. Requires CSRF. Persist the choice and publish `sharing.changed` transactionally. On success reconcile the switch immediately; when off, set all three summary counts to zero in the response/cache before refreshing summary. **Contract request:** include the updated counts in the response, or guarantee the subsequent summary read reflects the committed choice. |
| Summary refresh | `GET /me/sharing` contract request above. Re-read after successful setting change and after a sharing summary event. |
| Live summary | `GET /events` **exists** as authenticated SSE with `Last-Event-ID`, but currently streams crawl/download progress only. **Contract request:** add user-scoped `sharing.summary.updated` carrying no counts or personal record data; on receipt fetch `GET /me/sharing`. The setting's internal `sharing.changed` outbox event is not itself a browser event. |
| Errors | Shared `Error` body has `code`, `message`, `details`. Localize by `code`; never display server English `message`. `401` expires session; `403` forbidden; `429`/network/5xx retain previous values and offer retry. |

The summary endpoint must return counts only and must never return record titles,
photo URLs, usernames of contributors, identifiers, visibility details, or
another user's setting. Counts are computed from distinct eligible records;
turning sharing off immediately removes this user's contribution from the
shared pool and returns zero for all three sharing summary counts, while the
user's own observations remain available to them through their own access.
No API key, secret, OAuth token, private/friends/family record, or download is
ever shared. Flickr authorization and visibility rules remain in force.

## Actions

| Action | Effect |
| --- | --- |
| Load view | Fetch setting/summary. Never infer “off” from a failed request. |
| Turn on | Immediately `PUT /me/sharing` with `{enabled:true}`; show pending state, then apply response and refresh summary. No confirmation. |
| Turn off | Open confirmation modal. Cancel closes it and leaves setting unchanged. Confirm sends `{enabled:false}`; after success, immediately show off and zero all three summary counts, then refresh. Failure closes no state and restores enabled state. |
| Retry | Repeat only the failed setting/summary request. |
| Live refresh | Apply `sharing.summary.updated` by fetching summary; poll as fallback per Live updates. |

Unsafe requests send `X-CSRF-Token` equal to the `xf_csrf` cookie. Ignore a
second toggle while a write is in flight. Announce success and failure in the
shell toast/live region.

## States

State scope is per view. Keep page heading and privacy explanation during load
and summary failures where the relevant data is available.

### Normal

This is the ready view: show the saved sharing choice and all three summary
values. A user with no stored choice starts with sharing off. Counts use locale
number formatting; zero received records or saved calls display as `0`, not as
an empty block.

### Ready

Compatibility name for the normal state in the repository UI-spec gate; use the
same presentation and behavior defined under [Normal](#normal).

### Loading

If the initial response takes over 200ms, show a skeleton for the switch card
and three statistic values with `aria-busy=true`; keep a single status
announcement. After 10 seconds show Error. On refresh, retain existing values
and mark the summary busy rather than blanking it.

### Empty

When the request succeeds and the user has no observed records, show the
setting normally, “No shared records yet”, copy that public records appear as
the crawl discovers them, and CTA “Go to crawl” to `/crawl`. Keep “You share”
and “You receive” at 0; do not imply that enabling sharing creates records.

### Error

Initial load: show “Could not load sharing”, explain that the choice was not
changed, and provide Retry. Summary-only failure keeps the setting card and
shows an inline summary error and Retry. Write failure restores the previous
switch value and shows an action error. Never present a failed write as
successful.

### Partial and stale

Setting and summary are independent blocks. If one fails, preserve the other
and retry only the failed block. On SSE outage or summary data older than 30
seconds, keep last values, show “Updated {time} ago” and a stale indicator, and
poll `GET /me/sharing`. A stale count must not be described as current.

### Forbidden

401 follows shell expiry behavior and stops the event listener and poll. 403
shows the shared access-denied view. Do not reveal whether another user's
sharing or records exist. A CSRF rejection shows the localized action error
and restores the old switch state.

## Live updates

Reuse the shell's authenticated `GET /events` EventSource. On
`sharing.summary.updated`, fetch `GET /me/sharing`; ignore event payload data
other than event type and id. Resume with `Last-Event-ID`. If SSE is not
available or after three reconnect failures, poll the summary every 30 seconds
with backoff capped at 2 minutes. Stop polling while the document is hidden;
refresh immediately on visibility return. A successful local toggle refreshes
without waiting for an event. Show stale status after 30 seconds without a
successful summary read. No optimistic count changes except setting “You
share” to zero after a confirmed opt-out.

## Responsive

At 720px or less use one column, 16px page gutters, full-width cards, and wrap
summary statistics into two columns, then one column at 390px if needed. At
390px the switch label, help copy, modal actions and all three counts remain
readable without horizontal scrolling. The confirmation modal becomes a
bottom sheet with scrollable content and sticky actions. English and Vietnamese
copy may wrap; never clip fixed-height labels.

## Accessibility

Use one `h1` followed by card `h2`s. Implement the switch as a native checkbox
or button with `role="switch"`, accessible name “Share my crawled records”,
and synchronized `aria-checked`; explain scope with `aria-describedby`. The
confirmation uses a named modal dialog, focuses Cancel first, traps focus,
closes with Escape, and returns focus to the switch. Keep a visible 2px focus
ring and minimum 3:1 control-boundary contrast. Status includes text, not color
alone. Mark loading with `aria-busy`; announce one summary update politely and
do not announce each poll. Respect reduced motion.

## i18n

English and Vietnamese values cover every visible label, state message, action,
confirmation, and error. Dynamic values use named placeholders.

| Key | EN | VI |
| --- | --- | --- |
| user.sharing.title | Sharing | Chia sẻ |
| user.sharing.lead | Share eligible public records to reduce duplicate Flickr calls for everyone. | Chia sẻ dữ liệu công khai đủ điều kiện để giảm các lượt gọi Flickr trùng lặp cho mọi người. |
| user.sharing.choice.title | Share my crawled records | Chia sẻ dữ liệu đã thu thập của tôi |
| user.sharing.choice.on | Sharing is on | Đang bật chia sẻ |
| user.sharing.choice.off | Sharing is off | Đang tắt chia sẻ |
| user.sharing.choice.public | Only eligible public record data is shared. | Chỉ dữ liệu công khai đủ điều kiện được chia sẻ. |
| user.sharing.never.title | Never shared | Không bao giờ được chia sẻ |
| user.sharing.never.text | Private, friends-only and family-only records, API keys and secrets, OAuth tokens, and downloads are never shared. | Dữ liệu riêng tư, chỉ bạn bè, chỉ gia đình, khóa và mã bí mật API, mã thông báo OAuth và nội dung tải xuống không bao giờ được chia sẻ. |
| user.sharing.summary.title | What sharing gives you | Chia sẻ mang lại điều gì |
| user.sharing.summary.shared | You share | Bạn chia sẻ |
| user.sharing.summary.records | records visible to others | bản ghi hiển thị với người khác |
| user.sharing.summary.received | You receive | Bạn nhận được |
| user.sharing.summary.from-users | from other users | từ người dùng khác |
| user.sharing.summary.saved | Calls saved | Lượt gọi tiết kiệm |
| user.sharing.summary.week | this week | tuần này |
| user.sharing.off.notice | Sharing is off. You only see records crawled through your own access, and your crawl may repeat calls others already made. | Chia sẻ đang tắt. Bạn chỉ thấy dữ liệu được thu thập bằng quyền truy cập của mình và quá trình thu thập có thể lặp lại các lượt gọi mà người khác đã thực hiện. |
| user.sharing.confirm.title | Turn sharing off? | Tắt chia sẻ? |
| user.sharing.confirm.text | This takes effect as soon as you confirm. Other users will no longer see records contributed only by you. | Thay đổi có hiệu lực ngay sau khi bạn xác nhận. Người khác sẽ không còn thấy dữ liệu chỉ do bạn đóng góp. |
| user.sharing.confirm.off | Turn sharing off | Tắt chia sẻ |
| user.sharing.empty.title | No shared records yet | Chưa có dữ liệu được chia sẻ |
| user.sharing.empty.text | Public records will appear here as your crawl discovers them. | Dữ liệu công khai sẽ xuất hiện tại đây khi quá trình thu thập tìm thấy chúng. |
| user.sharing.empty.cta | Go to crawl | Mở trang thu thập |
| user.sharing.error.title | Could not load sharing | Không thể tải thông tin chia sẻ |
| user.sharing.error.text | Your sharing choice was not changed. Check your connection and try again. | Lựa chọn chia sẻ của bạn chưa thay đổi. Hãy kiểm tra kết nối rồi thử lại. |
| user.sharing.error.summary | Could not refresh sharing counts. | Không thể cập nhật số liệu chia sẻ. |
| user.sharing.error.update | Could not update your sharing choice. | Không thể cập nhật lựa chọn chia sẻ. |
| user.sharing.forbidden.title | You cannot view sharing settings | Bạn không thể xem cài đặt chia sẻ |
| user.sharing.forbidden.text | Your account does not have access to this page. | Tài khoản của bạn không có quyền truy cập trang này. |
| user.sharing.loading | Loading sharing | Đang tải thông tin chia sẻ |
| user.sharing.updated | Updated {time} ago | Cập nhật cách đây {time} |
| user.sharing.stale | Sharing counts may be out of date. | Số liệu chia sẻ có thể đã cũ. |
| user.sharing.toast.enabled | Sharing is on. | Đã bật chia sẻ. |
| user.sharing.toast.disabled | Sharing is off. | Đã tắt chia sẻ. |
| common.cancel | Cancel | Hủy |
| common.retry | Retry | Thử lại |

## Test IDs

E2E selectors use these stable `data-testid` values:

- `sharing-page`
- `sharing-setting-card`
- `sharing-toggle`
- `sharing-never-shared`
- `sharing-summary`
- `sharing-shared-count`
- `sharing-received-count`
- `sharing-calls-saved-count`
- `sharing-off-notice`
- `sharing-off-confirmation`
- `sharing-confirm-off`
- `sharing-empty-state`
- `sharing-loading-state`
- `sharing-error-state`
- `sharing-summary-retry`
- `sharing-updated-at`

## Out of scope

Changing Flickr permissions or privacy, browsing records contributed by a
particular person, contributor identity, sharing controls per record, admin
fleet-wide sharing management, and editing another user's setting.

## Data

Compatibility heading for the repository UI-spec gate; see [Data and API](#data-and-api).

## Interactions

Compatibility heading for the repository UI-spec gate; see [Actions](#actions).

## Test hooks

Compatibility heading for the repository UI-spec gate; see [Test IDs](#test-ids).
