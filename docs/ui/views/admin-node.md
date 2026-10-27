# Cluster node detail

View id `admin-node`. Route `/admin/nodes/:id`. Mockup:
[admin-node.html](../mockups/admin-node.html). Build ticket: JOOXF-181.
Baseline: [POC](../poc/xflickr-poc.html), `vNode()` and `previewWrap()`.
Follow [admin shell](admin-shell.md), [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md) and [i18n](../i18n.md).

## Purpose

Give ADMIN operators a detailed, live operational view of one control or crawl
node: its configuration, services, resource trends, assigned keys, temporary
downloads, history, events and incidents. ADMIN defaults to the dark
control-room theme; the optional light theme retains ADMIN density and geometry.

## Route and access

- Route `/admin/nodes/:id`, inside the admin shell. The breadcrumb returns to
  `/admin/nodes`.
- ADMIN only. Resolve `GET /me` first. A missing/expired session follows the
  shell login flow; a signed-in USER or API 403 sees the shell forbidden state.
  Do not request or retain node data after a 403.
- `id` is an opaque node identifier. An unknown id uses the view's not-found
  message and link to the node list; it is not treated as the first node.
- Shell owns navigation, theme, locale and Live/Paused. This view owns the
  selected node snapshot, assigned-key subset, histories and refresh/retry.

## Layout

At 1440 px use the 226 px admin rail and remaining content width. Show a
breadcrumb, node id, role/region/agent summary, a system specification panel,
health and operational flags, and Refresh. The specification panel shows OS,
architecture, CPU cores, RAM and disk totals, agent start time and WireGuard
address when present. The POC's probe action is shown for blocked/offline health;
Drain/Undrain and Restart agent remain available in the header. Show a warning
when this node's public egress IP is also reported by another enabled crawl
node.

The first row contains health, CPU, RAM, disk, network and Flickr-call
summaries. Follow with CPU/RAM and network time-series charts; crawl nodes also
show calls-per-second history and assigned keys. Show services for the control
node only, matching the POC. Show temporary downloads for either node role.
Then show heartbeat and public-IP history, node-scoped incidents, and the
eight newest node events. Control nodes have no Flickr egress or assigned
Flickr keys; show those values as “Not applicable”, not zero. Use existing
`panel`, `stat`, `pill`, `resource-bar`, `table`, `banner`, `button`,
`empty-block` and `skeleton` components only.

At 390 px the rail becomes its mobile drawer, action buttons wrap, summary
stats and panels stack, charts retain labels and scale to panel width, and
tables reflow into labelled records. No page-level horizontal scrolling.

## Data and API

Paths are relative to `/api/v1`. Current contract contains `GET /admin/quota`
only for the listed key fields; there are no node detail, history, incident,
node event-stream or node action routes yet. The routes and fields below marked
“contract request” are proposals for JOOXF-181. Do not call them before that
build ticket adds them to OpenAPI and backend. Every row states the source,
fields and action. Never expose join tokens, Flickr secrets, OAuth tokens or
unrelated user data.

| Content | Data source and fields | Action |
| --- | --- | --- |
| Spec sheet and identity | Contract request `GET /admin/nodes/{id}`: `id`, `role`, `region`, `agent_version`, `agent_started_at`, `os`, `architecture`, `cpu_cores`, `ram_total_bytes`, `disk_total_bytes`, `public_egress_ip`, `wireguard_ip`, `enabled`, `draining`, `health`, `generated_at`. `public_egress_ip` is blank on the control node. | Breadcrumb returns to node list. |
| Services | Same response: `services[] {name, state, checked_at, detail}` on control nodes only, as in the POC. Include only services configured for that node; state is `ok`, `degraded`, `down` or `unknown`. Detail is a safe short diagnostic, never a secret or raw log. | None; informational status. |
| Current resources | Same response: `cpu_percent`, `ram_used_bytes`, `disk_used_bytes`, `network_in_mbps`, `network_out_mbps`, `network_in_percent`, `network_out_percent`, `flickr_calls_per_second`, `workers_running`, `workers_capacity`, `heartbeat_at`, `http_429_last_hour`, `cooldown_until`. Resource percentages are 0–100 and capacity-relative; missing/zero capacity means unavailable, not 0%. | Refresh refetches snapshot. |
| Resource charts | Contract request `GET /admin/nodes/{id}/history?from={RFC3339}&to={RFC3339}&interval=10s`: `generated_at`, `points[] {at,cpu_percent,ram_used_bytes,disk_used_bytes,network_in_mbps,network_out_mbps,flickr_calls_per_second}`. Default range is last 60 minutes; downsample to at most 360 points. Call rate is absent on control nodes. | Refresh range; chart points have a text summary and are not individually focusable. |
| Assigned keys | Existing `GET /admin/quota`: `keys[] {key_id,label,user_id,node,used,limit,state,trend,reset_at}` filtered by exact `node == id`. Do not display secret or full public key. Control node has no assigned keys. | Key is informational; placement changes belong to Cluster nodes view. |
| Temporary downloads | Contract request `GET /admin/nodes/{id}/downloads`: `downloads[] {job_id,state,temp_size_bytes,expires_at}` for temporary files currently held by this node. Do not include user identity, file names or file contents. | Rows are informational; opening or cancelling a download belongs to the Downloads view. |
| Heartbeat history | Same history route: `heartbeat_points[] {at,state,agent_version}`; maximum 100 most recent transitions in the requested range. Current heartbeat and health come from detail snapshot. | None; history is read-only. |
| Public IP history | Same history route: `ip_points[] {at,public_egress_ip,change_reason}`; maximum 50 most recent changes. Empty for control node. | None; history is read-only. |
| Duplicate-egress warning | Compare detail `public_egress_ip` with `public_egress_ip` of other enabled crawl nodes in existing node-list contract request `GET /admin/nodes` (see admin-nodes spec). Warn only on exact normalized IP match; list conflicting node ids. Do not compare private WireGuard addresses. | Link to the other node's detail; no automatic reassignment. |
| Recent events | Contract request `GET /admin/nodes/{id}/events?limit=8`: `events[] {id,at,severity,category,message}` newest first. Limit fixed to 8; message is sanitized and must not contain credentials. | “View all” links to `/admin/events` with node filter only if that route supports it; until then omit the control. |
| Incidents | Contract request `GET /admin/nodes/{id}/incidents?limit=20`: `incidents[] {id,title,severity,state,opened_at,updated_at,summary}`. Return open incidents first then newest resolved; summary is safe text. | Link to `/admin/issues` only if supported route/query exists; otherwise rows are informational. Acknowledge/resolve are out of scope for this view. |
| Actions | Contract requests `POST /admin/nodes/{id}/probe`, `POST /admin/nodes/{id}/drain` `{draining:boolean}`, and `POST /admin/nodes/{id}/restart`; each returns `{accepted,requested_at}` and refreshed node state where applicable. Existing contracts do not define these routes. | Probe once; Drain/Undrain toggles admission; Restart queues one restart request. Confirm drain and restart. All require CSRF and audit outcome. |
| Live detail | Contract request `GET /admin/nodes/events` SSE, administrator-only, resumable event IDs. Events: `{channel,type,id,at,data}` with node `snapshot`/`upsert`/`remove` and resource point events. | Shell Live control pauses/resumes stream. |

Contract decisions: detail/history timestamps are RFC3339 UTC; history points
are ordered oldest-to-newest; missing metric samples are gaps, never zeroes.
Only enabled crawl nodes participate in duplicate-egress warnings. A changed
public IP is retained as history, not itself an incident. Services are omitted
for nodes with no reported service inventory and are shown only for the control
node, as in the POC. The POC's temporary-download panel is retained for both
node roles, with its source requested above and personal data omitted. The
control node may show its configured host/network metrics but never a Flickr
call-rate series, egress IP or assigned key list. There is no arbitrary date
picker; the fixed 60-minute chart range keeps the view bounded.

## Actions

| Action | Control | Result |
| --- | --- | --- |
| Back to nodes | Breadcrumb | Navigate to `/admin/nodes`. |
| Refresh | Header button | Refetch detail, quota and history; retain current content with refresh indicator. |
| Probe | Header button when blocked/offline | Submit one probe; show pending then resulting health. Disable while pending. |
| Drain/Undrain | Header toggle | Confirm; stop or resume accepting work, preserving assigned keys. |
| Restart agent | Header button | Confirm, enqueue request, show accepted time; never imply restart completed until heartbeat confirms it. |
| Open duplicate node | Warning link | Navigate to the conflicting node detail. |
| Live/Pause | Shell control | Pause closes SSE; resume reconnects from last event id. |

## States

### Normal

Render current snapshot, histories, services/keys as applicable, events and
incidents. Missing optional values use an em dash and “Unavailable”; never
invent zero values. Empty services, key, event and incident sections use their
section-specific empty copy. Display age from `generated_at`.

### Ready

Ready is the normal state above; this heading is retained as the catalogue
gate's state name.

### Loading

After the shared 200 ms threshold show page header and matching panel/chart
skeletons with `aria-busy=true` and a polite loading announcement. On refresh,
keep prior values visible and mark the snapshot refreshing.

### Empty

An empty node detail is not a valid successful resource. For a valid node with
no keys, services, temporary downloads, history, events or incidents, show a
concise section empty message and a Refresh CTA that retries that section. A
control node's key and public-IP sections say “Not applicable” and have no CTA.
A node list with no nodes uses the parent view's empty state.

### Error

If the node snapshot fails, show “Could not load this node.”, “Node details
could not be retrieved. Try again.” and Retry; keep a non-interactive skeleton
below. Focus the error heading after a second failed request. Section-only
failures follow Partial/stale. Never show raw status codes or traces.

### Partial/stale

If any secondary request fails, preserve the node snapshot and render an
inline section error with Retry. If detail fails but secondary responses
succeed, do not present secondary data as current; retain only previously
loaded values marked Stale. Mark snapshots stale after 30 seconds or when SSE
and fallback polling have failed. Include “Updated {age} ago”.

### Forbidden

A signed-in USER or any detail API 403 replaces content with the shell
forbidden page, closes SSE and clears cached node data. Copy: “You do not have
access to this page”, “This page is for administrators. Your account has the
User role.”, “Go to home”.

### Not found

For API 404 or unknown route id show “Node not found.”, “This node may have
been removed.” and “Back to nodes”. Do not substitute another node.

## Live updates

While the shell Live control is active, subscribe to administrator-only
`GET /admin/nodes/events`. Apply matching node snapshot/upsert/remove and
resource-point events. Reconnect with `Last-Event-ID`; pause closes the stream,
resume reconnects and applies the latest snapshot. Close on logout, forbidden,
route change or unmount.

If SSE is unavailable or has failed three reconnects, poll the node detail,
history and existing `/admin/quota` every 10 seconds. Back off 10, 20, 40, then
at most 60 seconds after failures. Stop polling in a hidden tab and fetch once
when visible. Do not poll when paused. Mark stale per States. Announce concise
health/service changes politely without moving focus or announcing each chart
sample.

## Responsive

At 1440 px use two chart columns, full key/history/event tables and admin rail.
At 390 px stack all panels, keep actions reachable, and reflow tabular data into
label/value records. Charts scale to container and retain visible time range
and series labels. No horizontal page scroll; IDs and IPs may wrap at safe
punctuation boundaries.

## Accessibility

- One h1; sections use headings and are named in a meaningful reading order.
- Health/service/incident status has text and icon as well as color. Resource
  charts have text summaries and labelled series; do not rely on hue alone.
- Tables use captions and scoped column headers; mobile reflow keeps each value
  associated with its label.
- Buttons have visible focus and accessible names; confirmations identify the
  node and effect. Use a polite live region for state changes, not chart ticks.
- Respect reduced motion. Touch targets are at least 44 px on mobile.

## i18n

Locale comes from the shell (`en`/`vi`). Use `Intl` with `en-US`/`vi-VN` for
numbers, timestamps and relative age; preserve node IDs, IP addresses, service
names and agent version. EN → VI keys:

| Key | EN | VI |
| --- | --- | --- |
| admin.node.title | Node detail | Chi tiết node |
| admin.node.back | Cluster nodes | Các node |
| admin.node.refresh | Refresh | Làm mới |
| admin.node.role | Role | Vai trò |
| admin.node.region | Region | Khu vực |
| admin.node.agent | Agent | Tác tử |
| admin.node.agent-started | Agent started | Agent đã khởi động |
| admin.node.spec | System specification | Cấu hình hệ thống |
| admin.node.os | Operating system | Hệ điều hành |
| admin.node.architecture | Architecture | Kiến trúc |
| admin.node.cpu-cores | CPU cores | Số lõi CPU |
| admin.node.wireguard-ip | WireGuard address | Địa chỉ WireGuard |
| admin.node.health | Health | Tình trạng |
| admin.node.health.ok | Healthy | Khỏe mạnh |
| admin.node.health.cooling | Cooling | Đang chờ hồi phục |
| admin.node.health.blocked | Blocked | Bị chặn |
| admin.node.health.offline | Offline | Ngoại tuyến |
| admin.node.enabled | Enabled | Đã bật |
| admin.node.disabled | Disabled | Đã tắt |
| admin.node.draining | Draining | Đang rút việc |
| admin.node.cpu | CPU | CPU |
| admin.node.ram | RAM | RAM |
| admin.node.disk | Disk | Ổ đĩa |
| admin.node.network | Network | Mạng |
| admin.node.inbound | Inbound | Lưu lượng vào |
| admin.node.outbound | Outbound | Lưu lượng ra |
| admin.node.calls | Flickr calls / second | Lượt gọi Flickr / giây |
| admin.node.chart.cpu-ram | CPU and RAM history | Lịch sử CPU và RAM |
| admin.node.chart.network | Network history | Lịch sử mạng |
| admin.node.chart.calls | Flickr calls per second history | Lịch sử lượt gọi Flickr mỗi giây |
| admin.node.chart.range | 60 min | 60 phút |
| admin.node.workers | Workers | Worker |
| admin.node.services | Services | Dịch vụ |
| admin.node.service-state | Service state | Trạng thái dịch vụ |
| admin.node.service.ok | Healthy | Khỏe mạnh |
| admin.node.service.degraded | Degraded | Suy giảm |
| admin.node.service.down | Down | Ngừng hoạt động |
| admin.node.service.unknown | Unknown | Không xác định |
| admin.node.keys | Assigned keys | Khóa được gán |
| admin.node.key | Key | Khóa |
| admin.node.downloads | Temporary downloads | Tải xuống tạm thời |
| admin.node.download.job | Job | Tác vụ |
| admin.node.download.state | State | Trạng thái |
| admin.node.download.size | Temporary size | Dung lượng tạm thời |
| admin.node.download.expires | Expires | Hết hạn |
| admin.node.empty.downloads.title | No temporary downloads | Không có tải xuống tạm thời |
| admin.node.empty.downloads.text | No temporary files are held by this node. | Node này không lưu tệp tạm thời nào. |
| admin.node.user | User | Người dùng |
| admin.node.used-limit | Used / limit | Đã dùng / giới hạn |
| admin.node.heartbeat-history | Heartbeat history | Lịch sử heartbeat |
| admin.node.ip-history | Public IP history | Lịch sử IP công khai |
| admin.node.events | Recent events | Sự kiện gần đây |
| admin.node.incidents | Incidents | Sự cố |
| admin.node.opened | Opened | Đã mở |
| admin.node.updated | Updated | Đã cập nhật |
| admin.node.egress-warning | Duplicate egress IP detected | Phát hiện IP đầu ra trùng lặp |
| admin.node.egress-warning.detail | This IP is also used by {nodeIds}. | IP này cũng được dùng bởi {nodeIds}. |
| admin.node.unavailable | Unavailable | Không khả dụng |
| admin.node.not-applicable | Not applicable | Không áp dụng |
| admin.node.empty.keys | No keys are assigned to this node. | Không có khóa nào được gán cho node này. |
| admin.node.empty.refresh | Refresh | Làm mới |
| admin.node.empty.services | No service inventory was reported. | Không có danh sách dịch vụ được báo cáo. |
| admin.node.empty.history | No history is available for this period. | Không có lịch sử trong khoảng thời gian này. |
| admin.node.empty.events | No recent events for this node. | Không có sự kiện gần đây của node này. |
| admin.node.empty.incidents | No incidents for this node. | Không có sự cố nào của node này. |
| admin.node.loading | Loading node details… | Đang tải chi tiết node… |
| admin.node.error.title | Could not load this node. | Không thể tải node này. |
| admin.node.error.text | Node details could not be retrieved. Try again. | Không thể tải chi tiết node. Hãy thử lại. |
| admin.node.error.retry | Retry | Thử lại |
| admin.node.section-error | This section could not be loaded. | Không thể tải phần này. |
| admin.node.stale | Stale | Đã cũ |
| admin.node.updated-age | Updated {age} ago | Cập nhật {age} trước |
| admin.node.forbidden.title | You do not have access to this page | Bạn không có quyền truy cập trang này |
| admin.node.forbidden.text | This page is for administrators. Your account has the User role. | Trang này dành cho quản trị viên. Tài khoản của bạn có vai trò Người dùng. |
| admin.node.forbidden.cta | Go to home | Về trang chủ |
| admin.node.not-found.title | Node not found. | Không tìm thấy node. |
| admin.node.not-found.text | This node may have been removed. | Node này có thể đã bị xóa. |
| admin.node.not-found.cta | Back to nodes | Quay lại danh sách node |
| admin.node.probe | Send probe | Gửi yêu cầu kiểm tra |
| admin.node.probe.pending | Probe requested | Đã yêu cầu kiểm tra |
| admin.node.probe.success | Probe completed | Đã hoàn tất kiểm tra |
| admin.node.probe.error | Probe failed | Kiểm tra không thành công |
| admin.node.drain | Drain | Rút node khỏi nhận việc |
| admin.node.undrain | Undrain | Cho node nhận việc lại |
| admin.node.drain.confirm | Change this node's drain state? | Thay đổi trạng thái rút việc của node này? |
| admin.node.restart | Restart agent | Khởi động lại agent |
| admin.node.restart.confirm | Request an agent restart? | Yêu cầu khởi động lại agent? |
| admin.node.restart.accepted | Restart request accepted | Đã chấp nhận yêu cầu khởi động lại |
| admin.node.action.error | Could not complete this action. Try again. | Không thể hoàn tất thao tác. Hãy thử lại. |
| admin.node.confirm | Confirm | Xác nhận |
| admin.node.cancel | Cancel | Hủy |
| admin.node.live-update | Node details updated | Chi tiết node đã được cập nhật |
| common.status.live | Live | Trực tiếp |
| common.status.paused | Paused | Tạm dừng |

## Test IDs

E2E `data-testid` contract:

- `admin-node-page`
- `admin-node-back`
- `admin-node-refresh`
- `admin-node-health`
- `admin-node-summary`
- `admin-node-resource-cpu`
- `admin-node-resource-ram`
- `admin-node-resource-disk`
- `admin-node-resource-network`
- `admin-node-resource-calls`
- `admin-node-chart-cpu-ram`
- `admin-node-chart-network`
- `admin-node-chart-calls`
- `admin-node-services`
- `admin-node-service-<serviceName>`
- `admin-node-assigned-keys`
- `admin-node-key-<keyId>`
- `admin-node-temp-downloads`
- `admin-node-download-<jobId>`
- `admin-node-heartbeat-history`
- `admin-node-ip-history`
- `admin-node-egress-warning`
- `admin-node-events`
- `admin-node-event-<eventId>`
- `admin-node-incidents`
- `admin-node-incident-<incidentId>`
- `admin-node-probe`
- `admin-node-drain`
- `admin-node-restart`
- `admin-node-loading`
- `admin-node-empty-<section>`
- `admin-node-error`
- `admin-node-error-retry`
- `admin-node-section-error-<section>`
- `admin-node-stale`
- `admin-node-forbidden`
- `admin-node-not-found`

## Out of scope

Node registration/provisioning, agent implementation, service control,
key-placement changes, incident acknowledgement/resolution, arbitrary chart
range selection, new OpenAPI/backend implementation, and other admin views.
Contract requests here are for JOOXF-181 only.

## Data

See Data and API above.

## Interactions

See Actions above.

## Test hooks

See Test IDs above; those `data-testid` values are the E2E contract.
