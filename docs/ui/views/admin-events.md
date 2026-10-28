# Admin Events and audit

View id admin-events. Route /admin/events. ADMIN role, dark control-room by
default with the explicit light theme supported. Mockup:
[admin-events.html](../mockups/admin-events.html). Build ticket JOOXF-183.
The approved baseline is Events & audit in the
[POC](../poc/xflickr-poc.html). Follow the [admin shell](admin-shell.md),
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), and [i18n](../i18n.md).

## Purpose

Show a live, searchable record of operational events and a separate audit
table for administrator actions. The page is read-only: it helps operators
understand what happened and when, without changing users, keys, queues,
workers, or incidents.

## Route and access

- Route /admin/events inside the ADMIN shell; page title “Events & audit”.
- ADMIN only. The shell resolves GET /me before rendering the route. A 401
  follows the shell sign-in flow. A user role or admin endpoint 403 displays
  the shell's public forbidden page and clears cached ADMIN data.
- ADMIN defaults to dark. Respect explicit light theme and render
  data-role="admin" with the active data-theme.
- The shell owns navigation, role, theme, language and its single event
  stream. This page owns filters, event and audit data, freshness, refresh
  and audit export.

## Layout

At 1440 px, use the shell's 226 px rail and remaining content width:

1. Page head with title, a short “Everything the system did and every admin
   change, newest first.” summary, last-updated time, a 1 hour / 24 hours / 7 days selector and Refresh.
2. Event stream panel with severity chips, source selector, text search, result
   count and newest-first event rows. Keep the shared shell Live control.
3. Audit trail panel with a separate search, newest-first table, UTC-aware
   timestamps and Export audit. Columns: time, actor, action, target and safe
   summary.

Use existing admin page-head, panel, event-feed, table, select, button,
skeleton, banner and empty-state patterns. Do not add a shared component. At
390 px, controls wrap below the title, event rows remain readable and audit
rows become labeled cards; no page-level horizontal scrolling.

## Data

Existing event and audit sources are limited to what the current OpenAPI and
server implement. The requested admin read, export and live contracts below
are not existing routes; the build ticket must add and review them before this
view can fetch live data.

## Data and API

Paths are relative to /api/v1. Do not call an absent route or infer event
history from the current user stream.

| Content | Data source and fields | Action |
| --- | --- | --- |
| User progress stream | Existing GET /events: authenticated SessionCookie, optional Last-Event-ID; streams crawl progress and download progress/ready events. The OpenAPI route and server route-access map scope it to USER. | Not used by this ADMIN view. |
| Audit writes | Existing audit_events table: id, actor, action, target, detail (JSONB), created_at. The PostgreSQL recorder inserts actions. There is no ADMIN read/list route. | Contract request below exposes a safe read model; never return raw detail JSON. |
| Operational event history | Contract request GET /admin/events?severity=&source=&q=&since=&until=&cursor=&limit=. ADMIN only. Response: generated_at, items[] with id, occurred_at, source, severity, message, and optional safe audit summary {actor_label, action, target_label}; next_cursor. Sort newest first with a stable cursor. Source values: cluster, quota, crawl, download, sync, ai, audit, auth. Severity values: critical, warning, success, info. These full enum values replace the POC shorthand so filters and API responses remain explicit; the UI localizes their labels. Search is a case-insensitive substring match across message and safe audit labels/action. Do not search raw detail. Default limit is 50, maximum 120. | Severity/source filters, search and refresh reload this list; live events are deduplicated by id. |
| Audit trail | Contract request GET /admin/audit-events?since=&until=&q=&cursor=&limit=. ADMIN only. Response: generated_at, items[] with id, created_at, actor_label, action, target_label and summary; next_cursor. Map the persisted actor and target to safe display labels. Omit detail JSON and redact secrets, tokens, emails and personal photo data. Default limit is 50, maximum 100. | Search the safe actor/action/target/summary fields; paginate newest first. |
| Audit export | Contract request GET /admin/audit-events/export with the current since, until and q filters. Returns CSV for all matching audit rows, not only the visible page; columns match the table. Define and enforce a server-side export cap in the API contract; return a clear error when exceeded. Neutralize spreadsheet formula prefixes. | Export audit downloads the filtered CSV. |
| ADMIN live event feed | Contract request: add resumable ADMIN-authorized events.feed and events.audit updates to the shell's one shared SSE connection. Payload follows the shared pattern {channel, type, id, at, data}; feed data carries the event item, audit data carries the safe audit row. Deduplicate an audit row received on both channels by id. | Shell Live control pauses or resumes updates; never open a second stream. |

The UI pattern documents a shared GET /api/v1/events/stream shape and
ADMIN-scoped channels, but the current OpenAPI exposes GET /events and the
server access map marks it USER-only. The two contracts do not match. The
build ticket must reconcile the path and authorization, then document the
ADMIN channels in OpenAPI before this page subscribes. The current events.audit
and events.issues channel names in UI patterns are interface plans, not
evidence that the server emits those channels today.

The default view covers the last 24 hours; users can choose 1 hour or 7 days.
The request uses since and until timestamps in UTC. Server retention and the
export cap are API-contract decisions for JOOXF-183. Do not claim older events
exist until the server confirms its retention policy.

Never expose Flickr keys, secrets, tokens, passwords, raw JSON details, user
email addresses, private photo data, or unredacted IP addresses. Treat event
messages as untrusted text and render them as text, not HTML.

## Actions

| Action | Control | Result |
| --- | --- | --- |
| Select range | Last hour, last 24 hours or last 7 days selector | Reload both sections with the selected UTC since/until range. |
| Filter severity | All, Critical, Warning, Success or Info chip | Reload the event list and retain the selected source and query. |
| Filter source | Cluster, quota, crawl, download, sync, AI, audit or auth selector | Reload the event list for the selected source. |
| Search events | Search field | Debounced text search across event message and safe audit summary. |
| Clear filters | Clear filters button | Reset severity, source, search and range to their defaults. |
| Refresh | Refresh button | Reload both sections and freshness while retaining filters. |
| Search audit | Audit search field | Search safe actor, action, target and summary fields. |
| Export audit | Export audit button | Download the server-filtered CSV for the selected audit range and query. |
| Pause/resume live | Shell Live control | Pause or resume the shared stream; manual refresh stays available. |

Do not provide acknowledgement, resolve, replay, delete, key or worker actions
on this page. Incident management belongs to Issues.

## Interactions

Severity and source controls have accessible names. Search submits after a
short debounce and Enter submits immediately. Filter changes preserve the
scroll position. A new event appears at the top without moving focus. If the
reader is below the newest row, show a “New events” button instead of moving
the list. Export reports progress and completion through the polite live
region; a failed export leaves the filters and current rows intact.

## States

### Normal

Show the event feed and audit rows newest first, selected range, current
filters and freshness. Severity uses a text label and icon, not color alone.
Audit rows show actor, action and target; raw detail is never visible.
Selecting a source or severity with no audit matches leaves the audit section
empty without clearing the event feed.

### Ready

Ready is the normal populated state. Keep filters and range when returning
during the same session.

### Loading

On first load, show event-feed and audit-table skeletons, set aria-busy on each
loading region and announce “Loading events…”. Do not render sample records as
live. During refresh, retain successful content and mark only the affected
section busy.

### Empty

If the request succeeds but filters match no events, show “No events match
these filters” with “Clear filters”. If there are no audit rows in range, show
“No audit entries in this range”. An empty audit table does not clear the
event feed.

### Error

If both required requests fail, show “Events could not load”, explain that
operational history is temporarily unavailable and provide “Try again”. A
section failure shows an inline error and Retry in that section. Never expose a
server message, raw detail or credentials.

### Partial and stale

Retain successful sections if the other request fails. Label retained data with
its last successful update and retry only the failed section. Show a Stale
marker when the last successful event update is older than 20 seconds or both
the shared stream and polling fallback fail. Do not append a synthetic event.

### Forbidden

A 403 from any ADMIN event or audit endpoint replaces page content with the
shared public forbidden page, clears cached ADMIN records and closes the
stream. A 401 follows the shell sign-in flow.

## Live updates

Subscribe through the shell's single administrator-authorized stream while
Live is enabled. Apply only events.feed and events.audit for this view, keep
the selected filters, deduplicate by event id and preserve Last-Event-ID after
reconnect. Close or pause consumption on logout, forbidden access, hidden tab
or a paused Live control. If the event list is scrolled away from the top,
buffer arrivals and expose the “New events” button; do not steal focus.

Until the ADMIN stream contract is implemented, poll the requested
GET /admin/events and GET /admin/audit-events endpoints every 10 seconds,
backing off to 20, 40 and at most 60 seconds after failures. Pause polling in a
hidden tab and refresh once when it becomes visible. The shell owns stream
reconnect and pause behavior. Announce one concise new-event count, not every
arriving row.

## Responsive

| Width | Behaviour |
| --- | --- |
| >= 1100 | Full filter row, event feed list and audit table. |
| 721–1099 | Panels stack; filters wrap and the audit table keeps all columns. |
| <= 720 | One-column layout; filters wrap below the title; audit rows become labeled cards. |
| 390 | Shell rail uses its mobile drawer; search and buttons stay visible; no horizontal page scroll. |

At 200% zoom, cards reflow and all fields remain available. Use the 390 px
mobile frame in the mockup to check wrapped filters and audit cards.

## Accessibility

- One h1 followed by h2 section headings; use semantic sections and a table
  caption for the audit trail.
- Severity is text plus an icon; do not rely on color alone.
- Event-feed rows are keyboard reachable when they open a destination. Arrow
  Up and Arrow Down move between visible rows; Enter follows an available
  destination.
- Search, selects, buttons and export have visible focus. Touch targets are at
  least 44 px on mobile.
- Loading regions use aria-busy; errors use role=alert; freshness uses
  role=status. Announce a concise new-event count only when Live is active and
  not paused.
- Respect reduced motion; a new row appears immediately without animation.

## i18n

Use the shell locale (EN source, VI translation). Localize numbers,
timestamps and relative freshness with Intl. Event messages, actor labels,
action codes, target labels and IDs are server data: do not translate them.
Copy keys:

| Key | EN | VI |
| --- | --- | --- |
| admin.events.title | Events & audit | Sự kiện & kiểm toán |
| admin.events.subtitle | Everything the system did and every admin change, newest first. | Hoạt động của hệ thống và thay đổi của quản trị viên, mới nhất trước. |
| admin.events.stream.title | Event stream | Luồng sự kiện |
| admin.events.audit.title | Audit trail | Nhật ký kiểm toán |
| admin.events.range | Time range | Khoảng thời gian |
| admin.events.range.hour | Last hour | Giờ qua |
| admin.events.range.day | Last 24 hours | 24 giờ qua |
| admin.events.range.week | Last 7 days | 7 ngày qua |
| admin.events.severity | Severity | Mức độ |
| admin.events.severity.all | All severities | Tất cả mức độ |
| admin.events.severity.critical | Critical | Nghiêm trọng |
| admin.events.severity.warning | Warning | Cảnh báo |
| admin.events.severity.success | Success | Thành công |
| admin.events.severity.info | Info | Thông tin |
| admin.events.source | Source | Nguồn |
| admin.events.source.all | All sources | Tất cả nguồn |
| admin.events.source.cluster | Cluster | Cụm máy |
| admin.events.source.quota | Quota | Hạn mức |
| admin.events.source.crawl | Crawl | Thu thập |
| admin.events.source.download | Download | Tải xuống |
| admin.events.source.sync | Sync | Đồng bộ |
| admin.events.source.ai | AI | AI |
| admin.events.source.audit | Audit | Kiểm toán |
| admin.events.source.auth | Authentication | Xác thực |
| admin.events.search | Search events | Tìm sự kiện |
| admin.events.search.placeholder | Filter text | Lọc nội dung |
| admin.events.audit-search | Search audit entries | Tìm bản ghi kiểm toán |
| admin.events.audit-search.placeholder | Actor, action or target | Người thực hiện, hành động hoặc đối tượng |
| admin.events.audit.caption | Recent administrator actions | Thao tác gần đây của quản trị viên |
| admin.events.clear-filters | Clear filters | Xóa bộ lọc |
| admin.events.refresh | Refresh | Làm mới |
| admin.events.export-audit | Export audit | Xuất nhật ký kiểm toán |
| admin.events.updated | Updated {time} ago | Cập nhật {time} trước |
| admin.events.updated.just-now | Updated just now | Vừa cập nhật |
| admin.events.loading | Loading events… | Đang tải sự kiện… |
| admin.events.events-count | {count} events | {count} sự kiện |
| admin.events.empty.context | Showing results for the selected filters. | Đang hiển thị kết quả theo bộ lọc đã chọn. |
| admin.events.empty.title | No events match these filters | Không có sự kiện phù hợp với bộ lọc |
| admin.events.empty.text | Change the time range or clear a filter to see more events. | Hãy đổi khoảng thời gian hoặc xóa bộ lọc để xem thêm sự kiện. |
| admin.events.empty.clear | Clear filters | Xóa bộ lọc |
| admin.events.audit-empty.title | No audit entries in this range | Không có bản ghi kiểm toán trong khoảng này |
| admin.events.audit-empty.text | Choose a wider range or clear the audit search. | Hãy chọn khoảng thời gian dài hơn hoặc xóa nội dung tìm kiếm kiểm toán. |
| admin.events.error.title | Events could not load | Không tải được sự kiện |
| admin.events.error.text | Operational history is temporarily unavailable. | Lịch sử vận hành tạm thời không khả dụng. |
| admin.events.retry | Try again | Thử lại |
| admin.events.partial | Some event data could not update. | Một số dữ liệu sự kiện chưa thể cập nhật. |
| admin.events.stale | Data is out of date. Last updated {time}. | Dữ liệu chưa mới. Cập nhật lần cuối {time} trước. |
| admin.events.new-count | {count} new events | {count} sự kiện mới |
| admin.events.new-button | New events | Sự kiện mới |
| admin.events.export.loading | Exporting audit… | Đang xuất nhật ký kiểm toán… |
| admin.events.export.error | Audit export could not be created. | Không thể tạo tệp xuất nhật ký kiểm toán. |
| admin.events.export.success | Audit export downloaded. | Đã tải nhật ký kiểm toán. |
| admin.events.time | Time | Thời gian |
| admin.events.actor | Actor | Người thực hiện |
| admin.events.action | Action | Hành động |
| admin.events.target | Target | Đối tượng |
| admin.events.summary | Summary | Tóm tắt |

## Test IDs

Use stable data-testid selectors: admin-events-page,
admin-events-title, admin-events-range, admin-events-severity,
admin-events-source, admin-events-search, admin-events-clear-filters,
admin-events-refresh, admin-events-updated, admin-events-feed,
admin-events-feed-row-{id}, admin-events-feed-count,
admin-events-new-count, admin-events-new-button, admin-events-audit,
admin-events-audit-search, admin-events-audit-table,
admin-events-audit-row-{id}, admin-events-export,
admin-events-skeleton-feed, admin-events-skeleton-audit,
admin-events-empty, admin-events-empty-cta, admin-events-error,
admin-events-error-retry, admin-events-partial, admin-events-stale,
admin-events-forbidden, admin-events-live-region.

## Test hooks

Selectors in Test IDs are stable across EN/VI and range filters. E2E tests
should prefer roles and accessible names for filters, search, Refresh, Retry
and Export audit, then use data-testid to set up stream, empty and error
states. Mock event data must be synthetic and contain no user secrets or
private data.

## Out of scope

Application code, OpenAPI/backend changes, incident acknowledgement or
resolution, event retention policy implementation, audit schema changes,
admin shell changes, other views and live environment checks. Missing admin
read/export/SSE contracts and export limits are requests for build ticket
JOOXF-183.
