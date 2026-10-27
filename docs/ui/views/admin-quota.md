# API quota

## Purpose

Give administrators a live control-room view of Flickr API quota by key and
crawl node. It explains hourly use, the person and sizes lane split, short-term
burst behaviour, skipped shared work, 429 responses, and cooldowns. The view
supports moving a key to a healthy node and pausing or resuming a key. The
approved visual baseline is the ADMIN quota preview in
[`../poc/xflickr-poc.html`](../poc/xflickr-poc.html).

## Route and access

- Route: `/admin/quota`; title: `API quota`.
- ADMIN role only. The API returns 401 for an expired session and 403 for a
  signed-in non-admin; follow the shared forbidden-page pattern.
- ADMIN defaults to dark. Explicit light theme is supported with the same
  dense ADMIN geometry and design-system tokens.
- Build ticket: JOOXF-182. This design ticket does not add routes or fields to
  OpenAPI.

## Layout

- Page head: title and concise policy summary (hourly limit, reserve, burst,
  person/sizes allocation); Refresh action.
- Per-key panel: compact table with key id and label, owner, node, used/cap
  progress and reserve marker, person and sizes calls, current burst/cap and
  recent burst sparkline, hourly history sparkline, reset/cooldown timer, 429
  count, skipped shared calls, state, and Pause/Resume and Move actions.
- Per-node panel: crawl node, assigned keys, aggregate used/cap, calls per
  second history, 429 count, health, and cooldown timer. Control nodes are not
  listed as Flickr egress nodes.
- Show a short policy note: a call needs both hourly and per-second capacity;
  eligible already-shared records are reused and counted as skipped calls.
- Move key opens the shared modal pattern. It identifies the owner, current
  node, and source IP; offers only enabled, healthy crawl nodes other than the
  current node; and requires confirmation. Empty target list disables submit
  and explains that no healthy target is available.
- At 1440px, retain the dense table and two-column node/chart panels. At mobile,
  render key and node rows as stacked labelled cards; actions wrap and no page
  level horizontal scrolling is allowed.

## Data and API

Paths below are relative to `/api/v1`. Never expose Flickr secrets, OAuth
tokens, or key material beyond the existing masked label. Existing responses
are used only for the fields they currently define. Missing fields and routes
are contract requests for JOOXF-182; the client must not call them until they
are added to OpenAPI and the backend.

| Content | Data source and fields | Action |
| --- | --- | --- |
| Existing key baseline | `GET /admin/quota`: `keys[].key_id`, `user_id`, `label`, `node`, `used`, `limit`, `reset_at`, `state`, `trend`. Existing state values: `ok`, `cooling`, `exhausted`. | Refresh refetches this response. |
| Key usage and lanes | Contract request: extend `AdminKeyQuota` with `person_used`, `sizes_used`, `burst_now`, `burst_limit`, `per_second_rate`, `skipped_shared`, `http_429_count`, `cooldown_until`, `paused`, `hourly_history[]` (`at`, `used`, `limit`), and `burst_history[]` (`at`, `calls`). Histories cover the current hour and last 60 seconds respectively; return newest point last. `used` and `limit` remain the current hourly used/cap. | Pause/Resume key through requested action below. |
| Node summary | Contract request `GET /admin/quota` response adds `nodes[]`: `node_id`, `health`, `key_ids[]`, `used`, `limit`, `calls_per_second`, `calls_per_second_history[]` (`at`, `calls`), `http_429_count`, `cooldown_until`, `generated_at`. Aggregate key usage on the node; include only enabled crawl nodes. | Move key action can choose a different healthy node. |
| Move targets | Contract request `GET /admin/nodes`: `nodes[].id`, `role`, `health`, `enabled`, `region`, `egress_ip`. Include enabled crawl nodes with `health=ok`, excluding current node. | Used by Move modal. |
| Move key | Contract request `POST /admin/nodes/{node_id}/keys/{key_id}/move`, JSON `{ "target_node_id": string }`; response includes `key_id`, `node`, `state`. Requires CSRF and an audit record on success or failure. | Confirm moves the key; on success update placement and show confirmation. |
| Pause key | Contract request `POST /admin/quota/keys/{key_id}/pause`, JSON `{ "paused": boolean }`; response includes `key_id`, `paused`, `state`. Requires CSRF and audit record. Pause stops new scheduler calls; in-flight calls finish. Resume returns the key to normal eligibility, still subject to quota and node health. | Pause/Resume button; failure keeps prior state and shows an error toast. |
| Live snapshots | Contract request admin channel `quota.keys` on existing multiplexed `GET /events/stream`; payload snapshot/upsert carries the same key and node fields plus `generated_at`. Channel is ADMIN-only. Existing documented USER-scoped stream does not currently provide this admin channel. | Subscribe while view is mounted; use shared live control. |

Cooldown is the later applicable key or node `cooldown_until`; display a
countdown derived from the timestamp and server time, and show `—` when absent
or elapsed. Do not infer a cooldown from `state`. Histories are read-only and
do not imply forecast values. A key can be cooling while its assigned node is
healthy; node health and key quota state remain distinct.

## Actions

| Action | Result |
| --- | --- |
| Refresh | Refetch key and node quota data; keep existing data visible while refreshing. |
| Move | Open modal, choose an enabled healthy crawl node, confirm, then show new node and egress IP. Cancel closes without change. |
| Pause / Resume | Call the requested pause endpoint; explain that pause stops new calls and does not cancel in-flight calls. |
| Retry | Retry failed block request; preserve other successfully loaded blocks. |
| Live toggle | Pause/resume applying live events using shared live-control behaviour. |

## Interactions

Compatibility heading for the shared UI spec gate. Every control is described
in [Actions](#actions).

## States

### Normal

Show key and node panels with complete data. Pair each state word with its
status styling; bars include numeric used/cap values and reserve is labelled.
An empty node list with keys still present is a partial state, not an empty
page.

### Ready

Ready is the normal populated state described above.

### Loading

Render the page head and matching stat/table skeletons after the shared 200ms
threshold. Set `aria-busy=true` and announce “Loading API quota…”. Keep it for at
least 400ms once shown. Refresh with existing data uses a small indicator and
does not replace content.

### Empty

If both keys and nodes are empty, show key icon, “No API keys yet”, and “Quota
data appears when a Flickr API key is added. Add a key from Admin defaults or
ask an administrator to configure one.” No CTA: this view cannot create a key.

### Error

If initial load fails, show “Could not load API quota.” and “The control node
did not return quota data. Try again.” with Retry. Keep dimmed, inert skeletons
below the banner. Move and pause action failures use an error toast and retain
the previous displayed values.

### Partial / stale

If one key/node block fails, retain the other block and show an inline error
with its own Retry. Mark a block Stale with its age after 20 seconds without a
successful snapshot (two 10-second refresh intervals), or immediately when
its live channel is down. Values remain visible and carry a textual Stale
marker. Missing optional history renders “History unavailable”; never replace
it with zero.

### Forbidden

401 follows session-expiry behaviour. 403 renders the shared ADMIN-only
forbidden page without key or node data. Do not present an empty state for
authorization failure.

## Live updates

Subscribe to ADMIN `quota.keys` on `GET /api/v1/events/stream`. Apply snapshot
events as replacement and upsert events by `key_id` or `node_id`; ignore
unknown event types. Reconnect with `Last-Event-ID`, using shared backoff
(1, 2, 4, 8, then 15 seconds with jitter). The shell Live toggle pauses
applying updates and buffers up to 200 events; resume applies them and refetches.

If SSE is unavailable after three failed reconnects, poll `GET /admin/quota`
and requested `GET /admin/nodes` every 10 seconds, with 10, 20, 40, then max
60-second retry backoff. Stop polling while the tab is hidden; fetch once on
return. Show Live/Paused and stale age as text. Update cooldown countdowns
locally each second without a network request.

## Responsive

At 1440px use the ADMIN rail, full key table and paired node/call-rate panels.
At 1100px and below, charts/panels become one column as specified by the
shared breakpoints. At 720px and below, shell rail becomes its mobile drawer,
key and node table rows stack as cards with visible field labels, action buttons
wrap, and charts retain readable axes. The 390px mockup demonstrates the mobile
layout. No page-level horizontal scroll.

## Accessibility

- Use semantic headings, table caption and scoped column headers on desktop;
  mobile cards expose each field label with its value.
- Every progress bar has an accessible name and numeric used/cap text. Include
  reserve threshold and chart series labels in text; color is never the only
  status cue.
- Charts have an accessible summary and data values available to assistive
  technology. Sparklines are decorative only when equivalent history text is
  provided.
- Move modal follows the shared dialog pattern: labelled dialog, focus trap,
  initial focus on target, Escape/cancel, focus returned to triggering button.
- All controls have visible 2px accent focus, 44px minimum touch target on
  mobile, and live state announced politely. Respect reduced motion.

## i18n

Use these keys for all visible text. Values are EN · VI.

| Copy key | English | Vietnamese |
| --- | --- | --- |
| `admin.quota.title` | API quota | Hạn mức API |
| `admin.quota.summary` | Per-key limits, lane use and node health | Giới hạn từng khóa, mức dùng theo làn và tình trạng node |
| `admin.quota.refresh` | Refresh | Làm mới |
| `admin.quota.keys` | Per key | Theo khóa |
| `admin.quota.nodes` | Per node | Theo node |
| `admin.quota.key` | Key | Khóa |
| `admin.quota.owner` | User | Người dùng |
| `admin.quota.node` | Node | Node |
| `admin.quota.used` | Used this hour | Đã dùng trong giờ này |
| `admin.quota.person` | Person lane | Làn người dùng |
| `admin.quota.sizes` | Sizes lane | Làn kích thước |
| `admin.quota.burst` | Burst | Tốc độ tức thời |
| `admin.quota.hourlyHistory` | Hourly history | Lịch sử theo giờ |
| `admin.quota.burstHistory` | Calls per second, last 60 seconds | Lượt gọi mỗi giây trong 60 giây qua |
| `admin.quota.resetsIn` | Resets in | Đặt lại sau |
| `admin.quota.cooldown` | Cool-down | Thời gian chờ |
| `admin.quota.http429` | HTTP 429 | HTTP 429 |
| `admin.quota.skippedShared` | Skipped (shared) | Đã bỏ qua (dùng chung) |
| `admin.quota.state` | State | Trạng thái |
| `admin.quota.statusOk` | OK | Bình thường |
| `admin.quota.statusCooling` | Cooling | Đang chờ |
| `admin.quota.statusExhausted` | Exhausted | Đã hết hạn mức |
| `admin.quota.health` | Health | Tình trạng |
| `admin.quota.healthy` | Healthy | Khỏe mạnh |
| `admin.quota.activeKeys` | Active keys | Khóa đang hoạt động |
| `admin.quota.nodeCallsPerSecond` | Calls per second, by node | Lượt gọi mỗi giây theo node |
| `admin.quota.keysAssigned` | Keys | Khóa |
| `admin.quota.callsHour` | Calls this hour | Lượt gọi trong giờ này |
| `admin.quota.callsSecond` | Calls / second | Lượt gọi / giây |
| `admin.quota.move` | Move | Di chuyển |
| `admin.quota.pause` | Pause | Tạm dừng |
| `admin.quota.resume` | Resume | Tiếp tục |
| `admin.quota.moveTitle` | Move key | Di chuyển khóa |
| `admin.quota.moveDescription` | Calls will use the target node’s IP. | Lượt gọi sẽ dùng địa chỉ IP của node đích. |
| `admin.quota.targetNode` | Target node | Node đích |
| `admin.quota.noHealthyTarget` | No healthy node is available. | Không có node khỏe mạnh nào khả dụng. |
| `admin.quota.confirmMove` | Move key | Di chuyển khóa |
| `admin.quota.cancel` | Cancel | Hủy |
| `admin.quota.pauseHint` | Pausing stops new calls; in-flight calls finish. | Tạm dừng chặn lượt gọi mới; lượt đang chạy sẽ hoàn tất. |
| `admin.quota.sharedHint` | Shared records are reused and skipped calls save quota. | Bản ghi dùng chung được tái sử dụng và lượt gọi bỏ qua giúp tiết kiệm hạn mức. |
| `admin.quota.loading` | Loading API quota… | Đang tải hạn mức API… |
| `admin.quota.emptyTitle` | No API keys yet | Chưa có khóa API |
| `admin.quota.emptyBody` | Quota data appears when a Flickr API key is added. Ask an administrator to configure one. | Dữ liệu hạn mức sẽ xuất hiện khi thêm khóa API Flickr. Hãy nhờ quản trị viên thiết lập khóa. |
| `admin.quota.errorTitle` | Could not load API quota. | Không thể tải hạn mức API. |
| `admin.quota.errorBody` | The control node did not return quota data. Try again. | Node điều khiển không trả về dữ liệu hạn mức. Hãy thử lại. |
| `admin.quota.retry` | Retry | Thử lại |
| `admin.quota.partial` | Some quota data could not be loaded. | Không thể tải một phần dữ liệu hạn mức. |
| `admin.quota.stale` | Stale · updated {age} ago | Cũ · cập nhật {age} trước |
| `admin.quota.historyUnavailable` | History unavailable | Không có lịch sử |
| `admin.quota.live` | Live | Trực tiếp |
| `admin.quota.pausedLive` | Paused | Đã tạm dừng |
| `admin.quota.forbiddenTitle` | You do not have access to this page | Bạn không có quyền truy cập trang này |
| `admin.quota.forbiddenBody` | This page is for administrators. | Trang này dành cho quản trị viên. |
| `admin.quota.moveSuccess` | Key moved to {node} | Đã chuyển khóa đến {node} |
| `admin.quota.pauseSuccess` | Key paused | Đã tạm dừng khóa |
| `admin.quota.resumeSuccess` | Key resumed | Đã tiếp tục khóa |
| `admin.quota.actionError` | Could not update the key. Try again. | Không thể cập nhật khóa. Hãy thử lại. |

## Test IDs

E2E selectors:

The following values are the `data-testid` attributes used by E2E tests:

- `admin-quota-page`
- `admin-quota-refresh`
- `admin-quota-live`
- `admin-quota-key-table`
- `admin-quota-key-row`
- `admin-quota-used`
- `admin-quota-person-used`
- `admin-quota-sizes-used`
- `admin-quota-burst-history`
- `admin-quota-hourly-history`
- `admin-quota-429-count`
- `admin-quota-skipped-count`
- `admin-quota-pause`
- `admin-quota-resume`
- `admin-quota-move`
- `admin-quota-move-dialog`
- `admin-quota-target-node`
- `admin-quota-confirm-move`
- `admin-quota-node-table`
- `admin-quota-cooldown`
- `admin-quota-empty`
- `admin-quota-error`
- `admin-quota-retry`
- `admin-quota-stale`
- `admin-quota-forbidden`

## Data

Compatibility heading for the shared UI spec gate. Data sources and contract
requests are specified in [Data and API](#data-and-api).

## Test hooks

Compatibility heading for the shared UI spec gate. E2E `data-testid` hooks are
listed in [Test IDs](#test-ids).

## Out of scope

Changing quota settings, editing a key's Flickr credentials, creating or
deleting keys, call-log browsing, node configuration/probing, quota policy
implementation, and any endpoint or OpenAPI/backend changes. Those belong to
their own build or design tickets. This view only displays quota policy and
offers the specified move and pause controls.
