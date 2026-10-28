# Admin Metrics

View id admin-metrics. Route /admin/metrics. ADMIN role, dark control-room by
default with the explicit light theme supported. Mockup:
[admin-metrics.html](../mockups/admin-metrics.html). Build ticket JOOXF-180.
The approved visual baseline is the Metrics view in the
[POC](../poc/xflickr-poc.html). Follow the [admin shell](admin-shell.md),
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), and [i18n](../i18n.md).

## Purpose

Show operational throughput, queue pressure, API latency, failures and AI
search performance over a selectable time range. Use the Metrics view to spot
changes and navigate to the owning operations screen. This view is read-only.

## Route and access

- Route /admin/metrics inside the ADMIN shell; title “Metrics”.
- ADMIN only. The shell resolves GET /me before rendering the route. A 401
  follows the shell sign-in flow. A user role or admin endpoint 403 displays
  the shell's public forbidden page and clears cached ADMIN data.
- ADMIN defaults to dark. Respect the explicit light theme and render
  data-role="admin" with the active data-theme.
- The shell owns navigation, role, theme, language, Live control and its single
  event stream. This page owns range selection, chart data, freshness and
  refresh.

## Layout

At 1440 px, use the shell's 226 px rail and the remaining content width:

1. Page head with title, a short “Operational trends” summary, last-updated
   time, a 1h / 24h range selector and Refresh.
2. Seven chart panels in a two-column grid: crawl throughput; queued calls by
   person and sizes lane; Flickr API p50/p95 latency; error rate; HTTP 429
   responses; captions per minute; search p95 latency.
3. Each panel has a visible title, units, time axis, labeled series and
   keyboard-focusable point tooltips. Tooltips show the local time and the
   value with units. No chart-only information is hidden from assistive
   technology: provide a concise chart summary and an ordered text alternative.

Use existing admin page-head, panel, chart, legend, tooltip, select, button,
badge, skeleton, banner and empty-state patterns. Do not add a new shared
component. At 390 px, controls wrap below the title, panels stack in reading
order, and chart labels simplify before any content is clipped.

## Data

API sources and missing history fields are specified in Data and API below.

## Data and API

Paths are relative to /api/v1. Existing contracts are used only for the fields
listed. The requested time-series contract below is absent from OpenAPI and
the backend; it is a contract request for the build ticket. Do not call an
absent route, infer timestamps, or present illustrative mockup values as live
data.

| Content | Data source and fields | Action |
| --- | --- | --- |
| Current API call summary | Existing GET /admin/calls/stats?window=1h or 24h: window; calls_per_hour[] (hour, outcome, calls); top_error_codes[] (code, calls); latency_by_method[] (method, p50_ms, p95_ms); retries_by_facet[] (facet, retries); completed_per_hour[] (hour, photos, people). | Refresh; range selector changes window. Link to /admin/quota for key state. |
| Current queue | Existing GET /admin/queue: backlog_by_plan[] (plan, lane, backlog_calls), oldest_item_age_seconds, ready_queue_sizes[] (queue, ready_jobs). Sum backlog_calls by the returned lane for the current lane totals. Keep ready queue counts grouped by queue; do not label them as lane counts. | Link to /admin/pipeline. |
| Snapshot freshness | Existing GET /admin/ops/status: generated_at. Use only as an operations freshness signal, not as a timestamp for calls or queue values. | Refresh all visible sources. |
| Requested metric history | Contract request GET /admin/metrics?window=1h or 24h. Response: generated_at, window, bucket_minutes, and points[] ordered oldest first. Each point has at, records_per_minute, total_calls, failed_calls, http_429_count, api_latency_p50_ms, api_latency_p95_ms, captions_per_minute, search_p95_ms, and lanes[] with lane (person or sizes), backlog_calls and ready_jobs. Use 1-minute buckets for 1h and 15-minute buckets for 24h. Error rate is failed_calls / total_calls; HTTP 429 calls are included in failed_calls and shown separately. Null latency means the bucket had no observations; do not turn null into zero. | Range selector loads the matching window. |
| Live metric points | Contract request: add an ADMIN-authorized metrics.snapshot update to the shell's single resumable admin SSE stream, with event id, generated_at, window and changed point(s). The currently contracted /events stream is user-scoped and does not provide these admin metrics. | Shell Live control pauses or resumes updates; never open a second stream. |

Existing calls_per_hour and latency_by_method are window summaries, not
timestamped histories. Existing queue responses are snapshots, not histories.
The current contract has no HTTP 429 time series, captions-per-minute series or
search p95 series. Use GET /admin/metrics and the admin SSE contract request
for the charts; existing endpoints may be used for a separate current-summary
row only after their response succeeds. Do not substitute Flickr error codes
for HTTP status codes, work-item completions for captions, or user-scoped SSE
for administrator data. Never display keys, secrets, tokens, user emails,
photo data or other personal information.

## Actions

| Action | Control | Result |
| --- | --- | --- |
| Change range | 1h / 24h selector | Refetch the selected window and keep the current chart visible while it loads. |
| Refresh | Refresh button | Refetch all visible sources and update freshness. |
| Open queue | View pipeline link | Navigate to /admin/pipeline. |
| Open quota | View quota link | Navigate to /admin/quota. |
| Open AI status | View AI index link | Navigate to /admin/ai. |
| Inspect chart point | Pointer hover or keyboard focus | Show its timestamp and value; keep the tooltip available to keyboard and screen-reader users. |
| Live updates | Shell Live control | Pause or resume the shared admin stream. Refresh remains available while paused. |
| Retry | Page or section Retry button | Retry only the failed source and preserve successful sections. |

No action changes queue, quota, key, worker, search-index or model state.

## Interactions

The range selector has an accessible label and submits on selection. Refresh
and retry buttons announce progress without moving focus. A chart tooltip can
be reached with Tab and closed with Escape. Activating a named view link
navigates to that view. The shell Live control governs the single stream.

## States

### Normal

Show all seven charts, current range, freshness, units, legends and accessible
summaries. Use line series with explicit gaps for null values. Queue lines
distinguish person and sizes. API latency distinguishes p50 and p95. Error rate
and HTTP 429 count are separate labeled panels. The last point is the latest
completed bucket, not a prediction.

### Ready

Ready is the normal populated state. Retain the selected range when returning
to the page during the same session.

### Loading

On first load, show page-head and seven chart-shaped skeletons, set aria-busy on
the content region and announce “Loading metrics…”. Do not render sample
numbers. During refresh or range change, retain successful values and mark
only the affected panels busy.

### Empty

When every source succeeds and the selected window has no observations, show
“No metrics for this range”, explain that charts fill when work is processed,
and offer “View pipeline” to /admin/pipeline. A single empty series is an
inline chart message; it does not clear other panels.

### Error

If all required requests fail, show “Metrics could not load”, explain that
operational data is temporarily unavailable, and provide “Try again”. A 401
or 403 follows Route and access, not retryable error handling. Never expose a
server message or credentials.

### Partial and stale

If a source fails, retain successful panels and show a localized inline error
and Retry in the affected panel. Retained data is labeled with its last
successful update. Show a stale banner when data is older than 30 seconds or
the shared live stream and polling fallback both fail. Keep values readable,
mark them stale, and do not append a zero-valued point.

### Forbidden

A 403 from an ADMIN metrics source replaces page content with the shared public
forbidden page. Clear cached ADMIN metrics and stop applying stream events.

## Live updates

Subscribe through the shell's single administrator-authorized stream while
Live is enabled. Apply only metrics.snapshot updates for the selected window,
deduplicate by event id and preserve Last-Event-ID after reconnect. Close or
pause consumption on logout, forbidden access, hidden tab or a paused Live
control. Refresh once when the tab becomes visible.

Until the admin stream contract exists, poll the requested GET /admin/metrics
snapshot every 15 seconds, backing off to 30 then 60 seconds after failures.
Pause polling in a hidden tab and refresh once on visibility. Existing
GET /admin/calls/stats, GET /admin/queue and GET /admin/ops/status can refresh
their own summaries, but cannot replace the requested time-series contract.
Announce a material freshness or error-state change once in a polite live
region; do not announce every moving point.

## Responsive

| Width | Behaviour |
| --- | --- |
| >= 1100 | Two-column chart grid; full time labels and legend. |
| 721–1099 | Two-column charts where space allows; shorter ticks and wrapped controls. |
| <= 720 | One-column charts; range and Refresh wrap below the title; compact ticks. |
| 390 | Shell rail uses its mobile drawer; controls and labels stay visible, chart points remain focusable, no page-level horizontal scroll. |

At 200% zoom, cards reflow and chart alternatives remain available. On mobile,
reduce visual tick density before reducing hit targets.

## Accessibility

- One h1 followed by h2 panel headings; use semantic sections.
- Charts have accessible names, a concise summary and an ordered timestamp/value
  text alternative. Series are identified by text and line style as well as
  color.
- Tooltips work with pointer and keyboard, expose timestamp and unit, and do
  not obscure the focused point. Do not require hover.
- Loading uses aria-busy; errors use role=alert; freshness uses role=status;
  updates use a polite atomic live region.
- Select, Refresh, Retry and navigation have visible focus. Touch targets are
  at least 44 px on mobile. Respect reduced motion.

## i18n

Use the shell locale (EN source, VI translation). Localize numbers, units,
timestamps, range labels and relative update age. Copy keys:

| Key | EN | VI |
| --- | --- | --- |
| admin.metrics.title | Metrics | Chỉ số |
| admin.metrics.subtitle | Operational trends across the last hour or day. | Xu hướng vận hành trong giờ hoặc ngày qua. |
| admin.metrics.range | Time range | Khoảng thời gian |
| admin.metrics.range.hour | Last hour | Giờ qua |
| admin.metrics.range.day | Last 24 hours | 24 giờ qua |
| admin.metrics.refresh | Refresh | Làm mới |
| admin.metrics.updated | Updated {time} ago | Cập nhật {time} trước |
| admin.metrics.stale | Data is out of date. Last updated {time}. | Dữ liệu chưa mới. Cập nhật lần cuối {time} trước. |
| admin.metrics.latestBucket | Latest bucket | Khoảng dữ liệu gần nhất |
| admin.metrics.milliseconds | Milliseconds | Mili giây |
| admin.metrics.responsesPerBucket | Responses per bucket | Phản hồi mỗi khoảng |
| admin.metrics.failedCalls | Failed calls | Lượt gọi lỗi |
| admin.metrics.throughput | Crawl throughput | Thông lượng thu thập |
| admin.metrics.recordsPerMinute | Records per minute | Bản ghi mỗi phút |
| admin.metrics.queue | Queue length by lane | Độ dài hàng đợi theo làn |
| admin.metrics.personLane | Person lane | Làn người |
| admin.metrics.sizesLane | Sizes lane | Làn kích thước |
| admin.metrics.backlog | Backlog calls | Lượt gọi đang chờ |
| admin.metrics.ready | Ready jobs | Tác vụ sẵn sàng |
| admin.metrics.latency | Flickr API latency | Độ trễ API Flickr |
| admin.metrics.p50 | p50 | p50 |
| admin.metrics.p95 | p95 | p95 |
| admin.metrics.errorRate | Error rate | Tỷ lệ lỗi |
| admin.metrics.http429 | HTTP 429 responses | Phản hồi HTTP 429 |
| admin.metrics.captions | Captions per minute | Chú thích mỗi phút |
| admin.metrics.captionsSeries | Captions / min | Chú thích / phút |
| admin.metrics.search | Search latency | Độ trễ tìm kiếm |
| admin.metrics.noData | No metrics for this range | Không có chỉ số trong khoảng này |
| admin.metrics.noDataText | Charts fill when the system processes work. Choose a wider range or check the pipeline. | Biểu đồ sẽ có dữ liệu khi hệ thống xử lý công việc. Hãy chọn khoảng thời gian dài hơn hoặc kiểm tra quy trình. |
| admin.metrics.emptyCta | View pipeline | Xem quy trình |
| admin.metrics.error.title | Metrics could not load | Không tải được chỉ số |
| admin.metrics.error.text | Operational data is temporarily unavailable. | Dữ liệu vận hành tạm thời không khả dụng. |
| admin.metrics.error.retry | Try again | Thử lại |
| admin.metrics.sectionError | This chart could not update. | Không thể cập nhật biểu đồ này. |
| admin.metrics.loading | Loading metrics… | Đang tải chỉ số… |
| admin.metrics.chartSummary | {series}: {latest} at {time}; {points} points in {range}. | {series}: {latest} lúc {time}; {points} điểm trong {range}. |
| admin.metrics.point | {series}: {value} at {time} | {series}: {value} lúc {time} |
| admin.metrics.viewQuota | View quota | Xem hạn mức |
| admin.metrics.viewAi | View AI index | Xem chỉ mục AI |

## Test IDs

Use stable data-testid selectors: admin-metrics-page,
admin-metrics-title, admin-metrics-range, admin-metrics-refresh,
admin-metrics-updated, admin-metrics-throughput,
admin-metrics-queue-person, admin-metrics-queue-sizes,
admin-metrics-latency, admin-metrics-error-rate, admin-metrics-http-429,
admin-metrics-captions, admin-metrics-search, admin-metrics-chart-tooltip,
admin-metrics-live-region, admin-metrics-skeleton, admin-metrics-empty,
admin-metrics-empty-cta, admin-metrics-error, admin-metrics-error-retry,
admin-metrics-stale, admin-metrics-section-error-{section},
admin-metrics-forbidden.

## Test hooks

Each selector in Test IDs is stable across EN/VI and time ranges. E2E tests
should prefer roles and accessible names for the range, Refresh, Retry and
navigation controls, then use data-testid for chart panels and state setup.

## Out of scope

Application code, OpenAPI/backend changes, alert configuration, dashboards in
OpenObserve, API call log, quota/key controls, queue controls, node management,
model selection and other views. Missing data and stream contracts are requests
for the build ticket and must be implemented and reviewed there.
