# Admin API call log

## Purpose

Give ADMIN a read-only, newest-first view of recorded Flickr API calls for operational diagnosis. Follow the API call log in the approved [POC](../poc/xflickr-poc.html), using safe call metadata only. The POC's sample values are illustrative.

## Route and access

- Route: /admin/log in the authenticated ADMIN shell. The catalogue id is admin-call-log; the existing E2E scenario remains web/e2e/admin-log.spec.ts.
- ADMIN only. A 401 follows the shell sign-in flow. A 403 clears cached call-log data and shows the shared forbidden page.
- The page does not change keys, users, accounts, nodes, calls or queues. No request is replayed.
- Keep the shared shell role, theme, language, Live control and single event stream.

## Layout

At 1440 px, use the 226 px ADMIN rail and the remaining content width:

1. Page heading, short explanation, last-updated timestamp, 1 hour / 24 hours / 7 days / 30 days range selector and Refresh.
2. Filter row with outcome chips, method and key selectors, search, and Clear filters. Wrap filters below the heading when needed.
3. Latency summary for the currently returned page: p50, p95 and sample count. Exclude quota_wait records from latency percentiles.
4. Newest-first call table with Time, Method, Key, User, Account, Node, Lane / facet, Status, Latency and Bytes. Show at most the newest 250 records per page; use Load older for the next cursor.
5. Read-only detail drawer for a selected call. It repeats the safe row fields and adds attempt, request id, HTTP status, Flickr status and error code. Never show a request or response body.

Use existing shell, page-head, panel, chip, select, table, badge, pill, skeleton, banner and drawer patterns. At 390 px the rail is hidden, filters wrap, and each call becomes a labeled card without page-level horizontal scrolling.

## Data

The current POC presents synthetic call data and is not a live source. The application records non-sensitive call metadata in the partitioned PostgreSQL api_call_log table. The table is maintained with a 30-day retention window. The same call also emits a structured flickr.call event to telemetry; OpenObserve is not the list endpoint's source of truth.

Render only API responses. Do not infer a missing identity, status, node or byte count. A null key/user/account is shown as Unassociated; an absent optional value is an em dash. Format timestamps in the selected locale and retain UTC in the detail view.

## Data and API

Paths are relative to /api/v1. There is no current ADMIN call-log list route or ADMIN calls.log stream channel in OpenAPI. The route and stream below are contract requests for JOOXF-182; do not call them as existing endpoints.

| Content | Current data and fields | UI action / contract request |
| --- | --- | --- |
| Call rows | Existing api_call_log fields include called_at, started_at, method, key_id, user_id, account_id, node_id, node, lane, facet, outcome, status, http_status, latency_ms, response_bytes, bytes, attempt, request_id, flickr_stat and flickr_error_code. | Request ADMIN-only GET /admin/call-log?since=&until=&outcome=&method=&key_id=&node_id=&lane=&q=&cursor=&limit=. Return generated_at, items[], next_cursor and retention_days=30. Each item has id, started_at, method, safe key_label, user_label, account_label, node_id, lane, facet, outcome, http_status, latency_ms, response_bytes, attempt, request_id, flickr_stat and error_code. Use a stable newest-first cursor over called_at and id. Default the range to the last 24 hours and limit to 250; cap range and records at the 30-day retention and 250 rows. |
| Safe identity labels | Existing records have local key_id, user_id, account_id and flickr_account values. The POC uses synthetic labels; the current record alone does not define safe display labels. | The contract must join server-side data and return an administrator-safe key label plus non-email user/account labels (fallback: opaque “User #…” / “Account #…”). The POC Target cell uses a Flickr account handle; this design replaces it with the separate masked Account label and never displays the remote handle. Never return an API key/secret, Flickr NSID, email address, OAuth material or raw flickr_account value. Null associations are “Unassociated”. |
| Status and latency | outcome, http_status, flickr_stat, latency_ms, response_bytes/bytes, attempt and flickr_error_code are stored when available. Current outcomes are success, failure, throttled and quota_wait. | Filter by outcome/method/key/node/lane and safe labels only. Show HTTP status beside outcome. Use response_bytes, falling back to bytes only when response_bytes is absent. Compute p50/p95 over the returned page, excluding quota_wait, and state the sample count. Do not turn a missing status into success. |
| Detail | request_id is an opaque correlation id. Error messages may include untrusted text. Call records intentionally exclude credential material and request/response bodies. | No separate detail endpoint is required if the list response contains the safe fields. If a detail route is added, it must be ADMIN-only and return only the same allowlisted metadata plus safe error code; do not return raw error messages, bodies, query strings, headers or credentials. |
| Live tail | The UI patterns define one resumable GET /events/stream and the calls.log channel, but the current OpenAPI GET /events is USER-scoped and does not establish an ADMIN stream contract. | Contract request: authorize calls.log for ADMIN on the shell's single SSE connection, with id, at and the safe call item; resume with Last-Event-ID. Until that contract exists, use the list endpoint's polling fallback. |

The 30-day figure is grounded in DefaultCallLogRetentionDays and the partition-maintenance code. The POC copy says OpenObserve; correct the source in this spec because the list is backed by api_call_log. OpenObserve still receives a separate structured event. This ticket does not change OpenAPI or application code.

## Actions

| Action | Result |
| --- | --- |
| Choose range, outcome, method, key, node or lane | Reload the list and keep the other filters. |
| Search | Match method, facet, node and safe user/account labels only; never search remote payloads or credentials. |
| Clear filters | Restore the last-24-hours range and default outcome/method/key filters. |
| Refresh | Reload the current page and freshness timestamp. |
| Open details | Show the selected row's allowlisted metadata in a read-only drawer. |
| Load older | Request the next opaque cursor, preserving sort and filters. |
| Pause / resume Live | Pause or resume consumption of the shared shell stream. |

## Interactions

- Filters have explicit labels; changing a filter preserves the selected time range. Search submits after a short debounce and Enter submits immediately.
- A new live row is added at the top only when the reader is at the top. Otherwise show a New calls button; activating it returns to the newest row without stealing focus.
- Detail opens from a named button in the row, not from a row-only click target. Escape closes the drawer and returns focus to that button.
- Load older is disabled while a page request is in flight and when next_cursor is null. Never auto-retry a page request or replay a Flickr call.

## States

### Normal

The populated state is the normal operating view; it uses the Ready content below.

### Ready

Show the selected range, filters, freshness, page latency summary and at most 250 safe rows. Preserve filters on refresh and return from details.

### Loading

On first load show table skeletons, set aria-busy on the results region and announce “Loading API calls…”. During refresh keep the last successful rows and mark only the results region busy.

### Empty

When the request succeeds with no matching rows, show “No API calls match these filters” and Clear filters. If there are no calls in the selected range, say “Calls appear after a key is used”; do not suggest starting crawl work from this diagnostic page.

### Error

If the list request fails, retain last-good rows as stale when available, show “API call log could not load” and Retry. Never show raw server errors or credentials.

### Partial and stale

If the list loads but the SSE stream is unavailable, keep the rows, show “Live updates paused” and poll the list every 5 seconds with backoff (5, 10, 20, then at most 60 seconds). Mark rows stale after two failed polls. Resume polling when the tab becomes visible and perform one immediate fetch. Do not append synthetic rows.

### Forbidden

A 403 clears the cached ADMIN call log and replaces page contents with the shared public forbidden state. A 401 follows the shell sign-in flow. Close the detail drawer and stream on logout or forbidden access.

## Live updates

Use the shared ADMIN-authorized SSE connection and calls.log channel when the contract request is implemented. Deduplicate by event id, preserve Last-Event-ID on reconnect, apply the active filters, and keep at most 250 visible rows. When SSE is blocked or has three failed reconnects, poll GET /admin/call-log every 5 seconds with the documented backoff. Stop polling while the tab is hidden; fetch once on return. The shell owns the stream and Live toggle; this view must not open a second EventSource.

## Responsive

- At 1440 px keep the 226 px rail, show the filters on one row when space permits, and render a compact table with no clipped labels.
- At 720 px and below hide the rail, wrap filters, hide decorative latency summaries before primary controls, and stack the detail drawer below the selected call.
- At 390 px render each call as a card with a visible label for each field. Keep buttons at least 44 px high, preserve reading order and avoid page-level horizontal scrolling.
- At 200% zoom all content and controls remain available by normal vertical scrolling.

## Accessibility

- Use a semantic table with a caption and scoped column headers on desktop. On mobile retain the table semantics and show each cell's visible field label.
- Buttons have visible text. Filters have associated labels; status includes text and icon/tone, never color alone.
- Announce loading, refresh completion and new-call count through a polite live region. Do not move focus when rows arrive.
- The details drawer is a labeled modal dialog with focus containment, Escape close and focus restoration. Respect reduced motion and browser zoom.

## i18n

All visible copy is keyed and supplied in English and Vietnamese. Preserve dynamic data as text and localize only labels, dates, numbers, statuses and units.

| Key | EN | VI |
| --- | --- | --- |
| admin.callLog.title | API call log | Nhật ký gọi API |
| admin.callLog.description | Recent Flickr API calls, newest first. Kept for 30 days. | Các lần gọi Flickr API gần đây, mới nhất trước. Lưu trong 30 ngày. |
| admin.callLog.range | Time range | Khoảng thời gian |
| admin.callLog.range.1h | Last hour | 1 giờ qua |
| admin.callLog.range.24h | Last 24 hours | 24 giờ qua |
| admin.callLog.range.7d | Last 7 days | 7 ngày qua |
| admin.callLog.range.30d | Last 30 days | 30 ngày qua |
| admin.callLog.time | Time | Thời gian |
| admin.callLog.updated | Updated {time} | Cập nhật {time} |
| admin.callLog.newestCalls | Newest calls · {count} shown · 250 max | Lần gọi mới nhất · hiển thị {count} · tối đa 250 |
| admin.callLog.sortedByTime | Sorted by time | Sắp xếp theo thời gian |
| admin.callLog.outcome | Outcome | Kết quả |
| admin.callLog.method | Method | Phương thức |
| admin.callLog.key | Key | Khóa |
| admin.callLog.user | User | Người dùng |
| admin.callLog.account | Account | Tài khoản |
| admin.callLog.node | Node | Node |
| admin.callLog.lane | Lane | Luồng |
| admin.callLog.facet | Facet | Nhóm tác vụ |
| admin.callLog.status | Status | Trạng thái |
| admin.callLog.latency | Latency | Độ trễ |
| admin.callLog.bytes | Bytes | Số byte |
| admin.callLog.search | Search calls | Tìm lần gọi |
| admin.callLog.search.placeholder | Method, node, user or account | Phương thức, node, người dùng hoặc tài khoản |
| admin.callLog.filter.allMethods | All methods | Tất cả phương thức |
| admin.callLog.filter.allKeys | All keys | Tất cả khóa |
| admin.callLog.laneFacet | Lane / facet | Luồng / nhóm tác vụ |
| admin.callLog.filter.all | All | Tất cả |
| admin.callLog.filter.success | Success | Thành công |
| admin.callLog.filter.errors | Errors | Lỗi |
| admin.callLog.clearFilters | Clear filters | Xóa bộ lọc |
| admin.callLog.refresh | Refresh | Làm mới |
| admin.callLog.live | Live | Trực tiếp |
| admin.callLog.paused | Paused | Đã tạm dừng |
| admin.callLog.p50 | p50 latency | Độ trễ p50 |
| admin.callLog.p95 | p95 latency | Độ trễ p95 |
| admin.callLog.sampleCount | calls on this page | lần gọi trên trang này |
| admin.callLog.details | Details | Chi tiết |
| admin.callLog.details.open | View details | Xem chi tiết |
| admin.callLog.details.title | Call details | Chi tiết lần gọi |
| admin.callLog.details.close | Close details | Đóng chi tiết |
| admin.callLog.loadOlder | Load older | Tải lần cũ hơn |
| admin.callLog.newCalls | New calls | Lần gọi mới |
| admin.callLog.loading | Loading API calls… | Đang tải lần gọi API… |
| admin.callLog.empty.title | No API calls match these filters | Không có lần gọi API nào khớp bộ lọc |
| admin.callLog.empty.body | Calls appear after a key is used. | Lần gọi xuất hiện sau khi có khóa được sử dụng. |
| admin.callLog.empty.hint | Try a wider time range or clear filters. | Hãy thử khoảng thời gian dài hơn hoặc xóa bộ lọc. |
| admin.callLog.empty.noRange | No results for this range | Không có kết quả trong khoảng thời gian này |
| admin.callLog.error.title | API call log could not load | Không thể tải nhật ký gọi API |
| admin.callLog.error.retry | Retry | Thử lại |
| admin.callLog.error.staleHint | Your last successful rows remain available when possible. Try again in a moment. | Các dòng tải thành công gần nhất vẫn được giữ nếu có. Hãy thử lại sau ít phút. |
| admin.callLog.error.refreshHint | Could not refresh | Không thể làm mới |
| admin.callLog.stale | Live updates paused | Đã tạm dừng cập nhật trực tiếp |
| admin.callLog.forbidden | You do not have access to this page. | Bạn không có quyền truy cập trang này. |
| admin.callLog.unassociated | Unassociated | Chưa liên kết |
| admin.callLog.unknown | Not available | Không có dữ liệu |
| admin.callLog.retention | 30-day retention | Lưu trữ 30 ngày |
| admin.callLog.pager | Page {page} | Trang {page} |
| admin.callLog.rowCount | {count} calls | {count} lần gọi |

## Test IDs

Keep these stable across themes and responsive states:

- admin-call-log-page, admin-call-log-heading, admin-call-log-live, admin-call-log-refresh, admin-call-log-range
- admin-call-log-outcome, admin-call-log-method, admin-call-log-key, admin-call-log-node, admin-call-log-lane, admin-call-log-search, admin-call-log-clear
- admin-call-log-summary, admin-call-log-results, admin-call-log-row-{id}, admin-call-log-details-{id}, admin-call-log-drawer
- admin-call-log-loading, admin-call-log-empty, admin-call-log-error, admin-call-log-stale, admin-call-log-forbidden
- admin-call-log-new-calls, admin-call-log-load-older

## Test hooks

Use the Test IDs above as the values of the `data-testid` attribute. Verify ADMIN authorization, 30-day bounds, stable pagination, null associations, filters, no credential or identity leakage, safe text rendering, hidden-tab polling, SSE reconnect/deduplication, mobile labels, keyboard drawer behavior and p50/p95 excluding quota_wait. Fixture rows must be synthetic and contain no credentials or personal account data.

## Out of scope

This ticket delivers the specification, static mockup and catalogue row only. Endpoint/SSE implementation, OpenAPI changes, E2E scenarios, export, direct OpenObserve access, user/key mutation, retrying calls, raw request/response data and live-data verification are out of scope. The existing build scenario is JOOXF-182.
