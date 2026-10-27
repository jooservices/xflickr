# Cluster nodes

View id `admin-nodes`. Route `/admin/nodes`. Mockup:
[admin-nodes.html](../mockups/admin-nodes.html). Build ticket: JOOXF-181.
Baseline: [POC](../poc/xflickr-poc.html), `vNodes()`, `nodeCard()` and
`previewWrap()`. Follow the [admin shell](admin-shell.md), [design
system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md) and
[i18n](../i18n.md).

## Purpose

Let ADMIN operators inspect node health and resource use, place API keys,
manage node availability, and issue a join token. ADMIN defaults to the dark
control-room theme; the explicit light theme keeps ADMIN geometry and density.

## Route and access

- Route: `/admin/nodes`, inside the admin shell. Node cards link to
  `/admin/nodes/:id`, owned by the separate Cluster node view.
- ADMIN only. Resolve `GET /me` before requesting node data. A missing session
  follows the shell's 401 login flow. A signed-in USER or admin API 403 renders
  the shell's forbidden page without node data.
- The shell owns navigation, theme/language controls, and Live/Paused control.
  This view owns node and key-placement data and its refresh.

## Layout

At 1440 px, use the shell's 226 px rail and remaining content width. Show the
page title, short description, Refresh and Add node actions, then a three-column
grid of node cards. Cards show identity and health, four resource rows, assigned
keys, worker count, heartbeat age, hourly 429 count and remaining cool-down.
Follow with a Key placement panel containing key, user, node, egress IP, calls
per second, state and Move action. Use existing `node-card`, `resource-bar`,
`pill`, `panel`, `table`, `button`, `empty-block`, `banner` and `skeleton`
components; do not add a component. At 390 px, the shell rail is its mobile
drawer, cards and key placements stack, controls wrap, and there is no page-level
horizontal scrolling.

## Data and API

Paths are relative to `/api/v1`. Existing routes/fields are identified below.
Every missing source is a contract request for JOOXF-181; do not call an absent
route or infer fields. Node snapshots include `generated_at`. Never expose node
join-token values after creation, Flickr secrets, OAuth tokens, or unrelated
personal data.

| Content | Data source and fields | Action |
| --- | --- | --- |
| Node identity | Contract request `GET /admin/nodes`: `nodes[].id`, `role` (`control`/`crawl`/`crawl_media`), `region`, `egress_ip`, `health` (`ok`/`cooling`/`blocked`/`offline`), `enabled`, `draining`, `agent_version`, `generated_at`. Egress IP is the public source address used for Flickr calls. | Open card to `/admin/nodes/:id`. |
| CPU, RAM, disk, network | Same requested response: `cpu_percent`, `ram_used_bytes`, `ram_total_bytes`, `disk_used_bytes`, `disk_total_bytes`, `network_in_mbps`, `network_out_mbps`. Show bars with text values and labelled meters. | Open node detail. |
| Keys assigned | Existing `GET /admin/quota`: `keys[].key_id`, `label`, `user_id`, `node`, `used`, `limit`, `state`, `trend`. Contract request extends node list with `keys_assigned` count; egress IP for the placement table comes from the node snapshot. | Move opens a confirmation modal; target choices are enabled, healthy crawl nodes other than the current node. |
| Workers | Contract request `GET /admin/nodes`: `workers_running`, `workers_capacity`. Display `running / capacity`. | Open node detail. |
| Heartbeat, 429 and cooldown | Contract request `GET /admin/nodes`: `heartbeat_at`, `http_429_last_hour`, `cooldown_until`. Calculate heartbeat age and remaining cooldown from server timestamps and local time; show zero/expired as `—`. | Probe action is available for blocked or offline nodes. |
| Key placement | Existing `GET /admin/quota` provides key id, label, user id, assigned node, usage and state. Node egress IP is from requested `GET /admin/nodes`. Existing quota response does not expose calls per second; contract request adds `calls_per_second` to each key entry in this view's response. | Move through requested `POST /admin/nodes/{node_id}/keys/{key_id}/move` with `{target_node_id}`. Audit actor, key id, old node, target node and outcome. |
| Probe | Contract request `POST /admin/nodes/{id}/probe`; return `health`, `probed_at`, `cooldown_until`. | Audit actor, node id, timestamp and outcome. |
| Drain | Contract request `POST /admin/nodes/{id}/drain` with `{draining: boolean}`. | Drain/Undrain; audit actor, node id, requested state and outcome. |
| Disable/enable | Contract request `PATCH /admin/nodes/{id}` with `{enabled: boolean}`. Reject disabling the control node and warn before disabling a node with assigned keys. | Disable/Enable; audit actor, node id, requested state and outcome. Keys on a disabled node must not make new calls; they are not silently reassigned. |
| Join token | Contract request `POST /admin/nodes/tokens` with `{role, region}`; response `{token, expires_at}`. Token is single-use, short-lived, shown once in a copyable dialog, and never persisted in browser storage or logs. | Create node token; audit actor, requested role/region, expiry and outcome, never token value. |
| Refresh and live snapshot | Contract request `GET /admin/nodes` and `GET /admin/nodes/events` (SSE). Existing `GET /admin/quota` supplies its listed key fields. | Refresh refetches node and quota data. Shell Live control governs SSE/polling. |

All five operator actions (probe, drain/undrain, disable/enable, move keys,
create node token) require CSRF protection and an audit record for success or
failure. Contract requests are proposals for the build ticket; no OpenAPI or
backend change is part of this design ticket.

## Actions

| Action | Control | Result |
| --- | --- | --- |
| Refresh | Refresh button | Refetch node and quota data; retain current values while refreshing. |
| Open node | Node card | Navigate to `/admin/nodes/:id`. |
| Probe | Card action for blocked/offline node | Request one health probe; show result and updated health. |
| Drain/Undrain | Node action menu | Stop admitting new work or restore admission; retain assigned keys. |
| Disable/Enable | Node action menu | Stop/start node participation. Disabling requires confirmation when keys are assigned. |
| Move key | Key placement row | Select an eligible target and confirm; show the resulting node/IP. |
| Create node token | Add node | Choose crawl role and region, then reveal the one-time token with Copy and Close. |
| Pause/resume live | Shell Live control | Pause applies no incoming updates; resume applies buffered/new snapshots. |

## States

### Ready

Show current node cards and key placement. Health values are `ok`, `cooling`,
`blocked` or `offline`; always pair a status word with its color. Cool-down is a
countdown while `cooldown_until` is in the future. A zero-node response uses the
Empty state.

### Loading

Render the page head and matching card/table skeletons after the shared 200 ms
threshold. Use `aria-busy=true`; announce “Loading cluster nodes…”. Keep existing
data visible with a small refresh indicator during refresh.

### Empty

When the successful node list is empty, show server icon, “No nodes joined”,
“Start the node agent on a VM with a join token.” and “Add node”. The CTA opens
the create-token flow.

### Error

If both node and quota data fail, show “Could not load cluster nodes.”,
“The control node did not return node data. Try again.” and “Retry” above a
dimmed, non-interactive skeleton. A second failure focuses the error heading.
Never show raw status codes or server traces.

### Partial and stale

If quota fails, keep node cards and show “Key placements unavailable” with a
section Retry. If nodes fail but quota succeeds, keep placements and show the
node error in its block. Mark retained data Stale with its update age when older
than twice the 10-second refresh interval, or when SSE is disconnected and
polling has failed. Keep last values visible.

### Forbidden

A signed-in USER or any admin request returning 403 replaces the view with the
shell forbidden page: “You do not have access to this page”, “This page is for
administrators. Your account has the User role.” and “Go to home”. Close SSE and
clear cached node data.

## Live updates

Subscribe to requested administrator-only `GET /admin/nodes/events` SSE while
the shell Live control is active. Events carry resumable IDs and
`{channel,type,id,at,data}`; apply node/key `upsert`, `remove` and `snapshot`
updates and reconnect with `Last-Event-ID`. Pause closes the stream; resume
reconnects and applies buffered snapshots. Close on logout or forbidden.

If SSE is unavailable, after three failed reconnects, or blocked, poll `GET
/admin/nodes` and existing `GET /admin/quota` every 10 seconds. Back off 10, 20,
40, then at most 60 seconds on errors. Stop polling when the tab is hidden and
fetch once when visible. Do not poll while paused. Announce concise updates
politely without moving focus.

## Responsive

At 1440 px show the full rail, three card columns and full key placement table.
At 390 px show the rail drawer, one card column, wrapped actions and key rows as
readable stacked fields. Keep node ids, health, resource values and actions
visible; do not truncate them or create page-level horizontal scrolling. At
720 px and below collapse the card grid to one column and reflow table rows.

## Accessibility

- One h1; card titles are headings within named sections. Cards are keyboard
  buttons with node id and health in the accessible name.
- Resource bars expose labelled meters and text values. Do not announce every
  live bar update; use one polite update region.
- Status always includes text and an icon as well as color. Cooldown and
  heartbeat include readable time text.
- Actions have visible focus, accessible names, confirmation for risky changes,
  and at least 44 px touch targets on mobile. Modal focus is trapped and
  restored; Escape closes it unless a token is currently being copied.
- Respect reduced motion; use semantic table headers and captions for key
  placement. Copy confirmation is announced with a status message.

## i18n

Locale is inherited from the shell (`en`/`vi`). Format numbers and relative time
with `Intl` (`en-US`/`vi-VN`); preserve node ids, IPs and role codes. EN → VI
copy keys:

| Key | EN | VI |
| --- | --- | --- |
| admin.nodes.title | Cluster nodes | Các node |
| admin.nodes.subtitle | Health, capacity and key placement across the cluster. | Tình trạng, năng lực và vị trí khóa trong cụm máy chủ. |
| admin.nodes.refresh | Refresh | Làm mới |
| admin.nodes.add-button | Add node | Thêm node |
| admin.nodes.role | Role | Vai trò |
| admin.nodes.region | Region | Khu vực |
| admin.nodes.egress-ip | Egress IP | IP đầu ra |
| admin.nodes.health | Health | Tình trạng |
| admin.nodes.health.ok | Healthy | Khỏe mạnh |
| admin.nodes.health.cooling | Cooling | Đang chờ hồi phục |
| admin.nodes.health.blocked | Blocked | Bị chặn |
| admin.nodes.health.offline | Offline | Ngoại tuyến |
| admin.nodes.cpu | CPU | CPU |
| admin.nodes.ram | RAM | RAM |
| admin.nodes.disk | Disk | Ổ đĩa |
| admin.nodes.network | Network | Mạng |
| admin.nodes.keys-assigned | Keys assigned | Khóa được gán |
| admin.nodes.workers | Workers | Worker |
| admin.nodes.heartbeat | Heartbeat | Nhịp tín hiệu |
| admin.nodes.heartbeat-age | {age} ago | {age} trước |
| admin.nodes.http-429 | HTTP 429 this hour | HTTP 429 trong giờ này |
| admin.nodes.cooldown | Cool-down | Thời gian chờ |
| admin.nodes.key-placement | Key placement | Vị trí khóa |
| admin.nodes.key | Key | Khóa |
| admin.nodes.user | User | Người dùng |
| admin.nodes.calls-per-second | Calls / second | Lượt gọi / giây |
| admin.nodes.move-key | Move key | Chuyển khóa |
| admin.nodes.probe | Send probe | Gửi yêu cầu kiểm tra |
| admin.nodes.drain | Drain | Rút node khỏi nhận việc |
| admin.nodes.undrain | Undrain | Cho node nhận việc lại |
| admin.nodes.disable | Disable | Tắt node |
| admin.nodes.enable | Enable | Bật node |
| admin.nodes.create-token | Create node token | Tạo token cho node |
| admin.nodes.copy-token | Copy token | Sao chép token |
| admin.nodes.token-created | Token created. Copy it now; it will only be shown once. | Đã tạo token. Hãy sao chép ngay; token chỉ hiển thị một lần. |
| admin.nodes.no-keys | No keys assigned | Chưa có khóa nào được gán |
| admin.nodes.empty.title | No nodes joined | Chưa có node nào kết nối |
| admin.nodes.empty.text | Start the node agent on a VM with a join token. | Khởi chạy node agent trên máy ảo bằng token kết nối. |
| admin.nodes.empty.cta | Add node | Thêm node |
| admin.nodes.loading | Loading cluster nodes… | Đang tải các node… |
| admin.nodes.error.title | Could not load cluster nodes. | Không thể tải các node. |
| admin.nodes.error.text | The control node did not return node data. Try again. | Node điều khiển chưa trả về dữ liệu node. Hãy thử lại. |
| admin.nodes.error.retry | Retry | Thử lại |
| admin.nodes.partial.title | Key placements unavailable | Vị trí khóa không khả dụng |
| admin.nodes.partial.retry | Retry key placements | Thử tải lại vị trí khóa |
| admin.nodes.stale | Stale | Đã cũ |
| admin.nodes.updated | Updated {age} ago | Cập nhật {age} trước |
| admin.nodes.forbidden.title | You do not have access to this page | Bạn không có quyền truy cập trang này |
| admin.nodes.forbidden.text | This page is for administrators. Your account has the User role. | Trang này dành cho quản trị viên. Tài khoản của bạn có vai trò Người dùng. |
| admin.nodes.forbidden.cta | Go to home | Về trang chủ |
| admin.nodes.confirm | Confirm | Xác nhận |
| admin.nodes.cancel | Cancel | Hủy |
| admin.nodes.copied | Token copied | Đã sao chép token |
| common.status.live | Live | Trực tiếp |
| common.status.paused | Paused | Tạm dừng |

## Test IDs

E2E `data-testid` contract:

- `admin-nodes-page`
- `admin-nodes-refresh`
- `admin-nodes-add-button`
- `admin-nodes-grid`
- `admin-nodes-row-<nodeId>`
- `admin-nodes-health-<nodeId>`
- `admin-nodes-resource-cpu-<nodeId>`
- `admin-nodes-resource-ram-<nodeId>`
- `admin-nodes-resource-disk-<nodeId>`
- `admin-nodes-resource-network-<nodeId>`
- `admin-nodes-keys-assigned-<nodeId>`
- `admin-nodes-workers-<nodeId>`
- `admin-nodes-heartbeat-<nodeId>`
- `admin-nodes-429-<nodeId>`
- `admin-nodes-cooldown-<nodeId>`
- `admin-nodes-probe-<nodeId>`
- `admin-nodes-drain-<nodeId>`
- `admin-nodes-disable-<nodeId>`
- `admin-nodes-key-placement`
- `admin-nodes-move-key-<keyId>`
- `admin-nodes-token-modal`
- `admin-nodes-token-copy`
- `admin-nodes-loading`
- `admin-nodes-empty`
- `admin-nodes-empty-cta`
- `admin-nodes-error`
- `admin-nodes-error-retry`
- `admin-nodes-partial-error`
- `admin-nodes-stale`
- `admin-nodes-forbidden`

## Out of scope

Node detail/history view, node provisioning and agent implementation, service
restarts, key creation/deletion, quota policy, bulk key balancing, audit-log
view, backend and OpenAPI implementation, and non-node admin views. Required
API contract requests are recorded above for JOOXF-181 only.

## Data

See Data and API above.

## Interactions

See Actions above.

## Test hooks

See Test IDs above; these `data-testid` values are the E2E contract.
