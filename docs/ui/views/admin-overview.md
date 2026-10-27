# Admin Overview

View id admin-overview. Mockup: [admin-overview.html](../mockups/admin-overview.html).
Build ticket: JOOXF-180. Baseline: [POC](../poc/xflickr-poc.html), vOverview(),
nodeCard(), issueRow() and evRow(). Follow the [admin shell](admin-shell.md),
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md) and [i18n](../i18n.md).

## Purpose

Give administrators an at-a-glance view of cluster health, quota, crawl backlog,
transfers, incidents and recent operations. Keep the POC KPI strip, throughput
chart, issue list and event feed. Add the required compact cluster mini-map;
detailed node cards and API latency remain on their dedicated views. ADMIN is
dark by default, with an explicit light theme.

## Route and access

- Route: /admin, inside [admin shell](admin-shell.md).
- ADMIN only. Resolve GET /me before fetching overview data. A missing session
  follows the shell's 401 login redirect. A user role or admin API 403 renders
  the shell's public 403 page with no dashboard data.
- Shell owns navigation, global theme/language controls and its own cluster
  footer. This page owns dashboard content and page refresh.

## Layout

At 1440 px, use the shell's 226 px rail and remaining content width. Start with
an h1 and brief status sentence, then:

1. Six KPI cells: healthy nodes, keys at quota, oldest queue age, running
   downloads/syncs, open incidents and crawl records per minute.
2. Two-column row: crawl throughput line chart (records/minute, last 60 minutes)
   and cluster mini-map. The map shows control/crawl nodes, health labels and
   connected links; it is a status diagram, not an editor.
3. Two-column row: up to five open incidents and up to nine newest events, each
   with a link to its dedicated view.

Use existing stats, panel, pill, banner, empty-block, skeleton, chart and button
components. The map is an inline diagram inside a panel, not a new shared
component. At 390 px, stack panels in reading order, wrap KPIs to two columns,
preserve labels and values, and avoid horizontal scrolling.

## Data and API

Paths are relative to /api/v1. Existing contracts are marked existing. Missing
sources are contract requests for JOOXF-180; do not call an absent route or
invent fields before that contract lands. Snapshots include generated_at.

| Content | Data source and fields | Action |
| --- | --- | --- |
| Healthy nodes KPI and map | Contract request GET /admin/overview: cluster.nodes[] with id, role (control/crawl), health (ok/degraded/offline), region, calls_per_second; cluster.links[] with from, to, status. Count health=ok. No IPs or private infrastructure details. | Node to /admin/nodes/:id; View nodes to /admin/nodes. |
| Keys at quota | Existing GET /admin/ops/status: keys[].status, used, remaining, limit. Count remaining=0, regardless of key status. | View quota to /admin/quota. |
| Queue age | Existing GET /admin/queue: oldest_item_age_seconds, backlog_by_plan[] (plan, lane, backlog_calls), ready_queue_sizes[] (queue, ready_jobs). | View pipeline to /admin/pipeline. |
| Running downloads/syncs | Contract request GET /admin/overview: transfers.downloads_running, transfers.syncs_running. Do not derive from user-scoped lists. | View downloads to /admin/downloads; View sync to /admin/sync. |
| Open incidents KPI and alert summary | Contract request GET /admin/overview: incidents.open_count, incidents.items[] (id, severity: critical/warning/info, title, summary, created_at), up to five newest open incidents. | Incident title to /admin/issues; All issues to /admin/issues. No acknowledge/resolve here. |
| Throughput chart and KPI | Contract request GET /admin/overview: throughput.records_per_minute[] (60 points, oldest first), latest_records_per_minute. | View metrics to /admin/metrics. Tooltip shows timestamp and value. |
| Recent events | Contract request GET /admin/overview: events[] (id, occurred_at, severity, source, message), newest first, up to nine. Redact secrets and personal data. | All events to /admin/events. Informational rows. |
| Freshness | Existing GET /admin/ops/status: generated_at; requested overview: generated_at. Use the oldest required-section timestamp for page freshness. | Refresh reloads all sources. |

The requested administrator-only overview snapshot combines the new fields above,
returns a consistent generated_at and per-section completeness for partial
rendering. Continue using existing ops/status and queue endpoints for fields
listed above. Never expose Flickr secrets, tokens, user email addresses or
private photo data. Use the common Error response and 401/403 auth responses.

## Actions

| Action | Control | Result |
| --- | --- | --- |
| Refresh | Refresh button | Refetch sources and retain current content while refreshing. |
| Open node | Map marker or accessible node list | Navigate to /admin/nodes/:id. |
| Open dedicated view | Named link for quota, pipeline, metrics, issues, events, downloads or sync | Navigate to the route in Data and API. |
| Open incident | Incident title | Navigate to /admin/issues. |
| Pause/resume live | Shell Live control | Shell pause governs this view; manual refresh remains available. |

No destructive or incident-management action is present here.

## States

### Ready

This is the normal dashboard state. Show six KPIs, chart, map, alerts and events. Zero incidents uses an empty block
inside its panel. Show page update time. A section can independently show empty
or error while the rest remains usable.

### Loading

First load: title and skeletons matching KPI, chart, map, alerts and events; no
fake values; content region has aria-busy=true. Refresh retains the last
snapshot and shows progress on Refresh.

### Empty

If all sources succeed and no nodes/activity exist, show “No cluster activity
yet”, “Join a crawl node to start seeing cluster health and work here.” and
“View nodes” to /admin/nodes. Zero incidents alone is not page-empty: show “No
open incidents” and “Everything is running normally.” No events: “No events yet”
and “Events appear when the system starts processing work.”

### Error

If every source fails, show “Could not load the admin overview.”, “The control
node did not return dashboard data. Try again.” and “Retry”, above a dimmed,
non-interactive skeleton. A 401/403 follows Route and access, not retryable
error handling.

### Partial and stale

On one-section failure, show “Data unavailable” and a section Retry; keep other
sections current. Mark retained data Stale. If the newest required snapshot is
older than 20 seconds, or both live and polling fail, preserve last values and
show “Updated {age} ago”. A 403 always switches to Forbidden.

### Forbidden

A user role or any admin endpoint 403 replaces content with the shell's public
forbidden page: “You do not have access to this page”, “This page is for
administrators. Your account has the User role.” and “Go to home” to /. Close
streams and do not render cached admin data.

## Live updates

Live is enabled by the shell Live control. Request an admin overview event
stream as a contract request; existing GET /events is user-scoped and cannot
supply admin metrics/events. The requested stream sends resumable IDs and
section snapshots for nodes, quota, queue, transfers, incidents, throughput
and events. Reconnect with Last-Event-ID; close on pause, logout or forbidden.
Poll existing /admin/ops/status and /admin/queue plus requested overview every
10 seconds if SSE is unavailable, backing off to 60 seconds. Do not poll while
paused. Update only the changed section and freshness timestamp. Announce
updates politely without moving focus.

## Responsive

At 1440 px use full shell rail, six KPI cells and two-column panel rows. At
390 px the shell rail is its mobile drawer; stack panels, use two KPI columns,
keep the map's accessible node list and scale chart to width. Do not introduce
page-level horizontal scrolling. At 720 px and below reduce chart labels first.

## Accessibility

- One h1; panel titles are h2. Use semantic sections and heading relationships.
- KPI values have labels and tabular numerals. Pair status colors with text and
  icons; never rely on color alone.
- Give chart an accessible name, summary and hidden ordered timestamp/value
  alternative. Tooltips support keyboard focus and pointer hover.
- Give the mini-map a summary and parallel node/status list; SVG links are
  decorative. Use time with datetime for incident/event timestamps.
- Refresh, retry and navigation have visible focus and accessible names; mobile
  targets are at least 44 px. Respect reduced motion. Use a polite update region,
  not a live region around the whole dashboard.

## i18n

Set document language from shell locale (en/vi). Localize relative time, number
formatting and plurals. EN → VI copy keys:

| Key | EN | VI |
| --- | --- | --- |
| admin.overview.title | Overview | Tổng quan |
| admin.overview.subtitle | Cluster, quota, work and incidents at a glance. | Tổng quan về cụm máy chủ, hạn mức, công việc và sự cố. |
| admin.overview.loading | Loading dashboard… | Đang tải bảng điều khiển… |
| admin.overview.controlNode | Control | Node điều khiển |
| admin.overview.crawlNode | Crawl node | Node thu thập |
| admin.overview.healthy | Healthy | Khỏe mạnh |
| admin.overview.degraded | Degraded | Suy giảm |
| admin.overview.offline | Offline | Ngoại tuyến |
| admin.overview.critical | Critical | Nghiêm trọng |
| admin.overview.warning | Warning | Cảnh báo |
| admin.overview.info | Info | Thông tin |
| admin.overview.queuedCount | {count} queued | {count} đang chờ |
| admin.overview.runningNow | Running now | Đang chạy |
| admin.overview.refresh | Refresh | Làm mới |
| admin.overview.updated | Updated {age} ago | Cập nhật {age} trước |
| admin.overview.stale | Stale | Đã cũ |
| admin.overview.nodesHealthy | Nodes healthy | Node khỏe mạnh |
| admin.overview.keysAtQuota | Keys at quota | Khóa đã hết hạn mức |
| admin.overview.queueAge | Oldest queue item | Mục chờ lâu nhất |
| admin.overview.transfersRunning | Downloads / syncs running | Tải xuống / đồng bộ đang chạy |
| admin.overview.openIncidents | Open incidents | Sự cố đang mở |
| admin.overview.recordsPerMinute | Records / min | Bản ghi / phút |
| admin.overview.throughput | Crawl throughput | Thông lượng thu thập |
| admin.overview.clusterMap | Cluster map | Sơ đồ cụm máy chủ |
| admin.overview.alertSummary | Alerts | Cảnh báo |
| admin.overview.recentEvents | Recent events | Sự kiện gần đây |
| admin.overview.allIssues | All issues | Tất cả sự cố |
| admin.overview.allEvents | All events | Tất cả sự kiện |
| admin.overview.viewNodes | View nodes | Xem các node |
| admin.overview.viewQuota | View quota | Xem hạn mức |
| admin.overview.viewPipeline | View pipeline | Xem quy trình |
| admin.overview.viewMetrics | View metrics | Xem số liệu |
| admin.overview.viewDownloads | View downloads | Xem lượt tải xuống |
| admin.overview.viewSync | View sync | Xem đồng bộ |
| admin.overview.noActivityTitle | No cluster activity yet | Chưa có hoạt động cụm máy chủ |
| admin.overview.noActivityBody | Join a crawl node to start seeing cluster health and work here. | Kết nối node thu thập để xem tình trạng cụm máy chủ và công việc tại đây. |
| admin.overview.noIncidentsTitle | No open incidents | Không có sự cố đang mở |
| admin.overview.noIncidentsBody | Everything is running normally. | Mọi thứ đang hoạt động bình thường. |
| admin.overview.noEventsTitle | No events yet | Chưa có sự kiện |
| admin.overview.noEventsBody | Events appear when the system starts processing work. | Sự kiện sẽ xuất hiện khi hệ thống bắt đầu xử lý công việc. |
| admin.overview.emptyCta | View nodes | Xem các node |
| admin.overview.errorTitle | Could not load the admin overview. | Không thể tải tổng quan quản trị. |
| admin.overview.errorBody | The control node did not return dashboard data. Try again. | Node điều khiển chưa trả về dữ liệu tổng quan. Hãy thử lại. |
| admin.overview.retry | Retry | Thử lại |
| admin.overview.sectionError | Data unavailable | Dữ liệu không khả dụng |
| admin.overview.forbiddenTitle | You do not have access to this page | Bạn không có quyền truy cập trang này |
| admin.overview.forbiddenBody | This page is for administrators. Your account has the User role. | Trang này dành cho quản trị viên. Tài khoản của bạn có vai trò Người dùng. |
| admin.overview.goHome | Go to home | Về trang chủ |
| admin.overview.chartDescription | Records per minute trend | Xu hướng số bản ghi mỗi phút |
| admin.overview.liveUpdate | Dashboard updated | Bảng điều khiển đã cập nhật |

## Test IDs

E2E data-testid contract:

- admin-overview-page
- admin-overview-refresh
- admin-overview-updated
- admin-overview-kpis
- admin-overview-kpi-nodes
- admin-overview-kpi-keys
- admin-overview-kpi-queue-age
- admin-overview-kpi-transfers
- admin-overview-kpi-incidents
- admin-overview-kpi-throughput
- admin-overview-throughput
- admin-overview-cluster-map
- admin-overview-node-{id}
- admin-overview-alerts
- admin-overview-events
- admin-overview-empty
- admin-overview-error
- admin-overview-retry
- admin-overview-stale
- admin-overview-forbidden

## Out of scope

Node provisioning/editing, quota changes, pipeline controls, incident
acknowledge/resolve, event search, download/sync management, detailed metrics,
API log inspection, backend implementation and OpenAPI edits. This spec records
contract requests only; endpoint/schema delivery belongs to the build ticket.

## Data

See Data and API above.

## Interactions

See Actions above.

## Test hooks

See Test IDs above; these data-testid values are the E2E contract.
