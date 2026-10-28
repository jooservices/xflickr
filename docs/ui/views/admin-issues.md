# Admin issues

## Purpose
Give ADMIN one live queue for operational incidents. Follow the approved POC. Cover the eleven flow cases named by JOOXF-160; infrastructure detector cases may reuse this view after their separate detector work lands.

## Route and access
- Route /admin/issues; ADMIN only; default dark control-room theme, with existing ADMIN light theme.
- Require an authenticated ADMIN session for every request. Show forbidden for 403 and return no incident data to a USER.
- Map stored open, acked, closed states to Open, Acknowledged, Resolved. Resolved is the UI label for closed.

## Layout
- Heading, live/stale indicator, counts for Open/Acknowledged/Resolved, status tabs, severity filter, search, and incident list.
- Columns: severity, area, issue/detail, first seen, last seen, count, status, actions. Sort newest last_seen first; offer Oldest first.
- A selected row opens a desktop detail drawer or mobile full-screen sheet with detection, automatic action, human step, timestamps, count, status, acknowledger, close time, and auto-closed marker when supplied.
- Pair severity text with icon/color. Never render raw detail JSON, credentials, full keys, tokens, or unredacted provider responses. Show safe key label only when supplied by the API.

## Data
Existing and requested incident fields/endpoints are defined below in Data and API.

## Data and API
### Existing implementation
The internal manager in backend/internal/ops/domain/incident.go, backend/internal/ops/app/incidents.go, backend/internal/ops/adapters/postgres/incidents.go, and backend/migrations/20261028120000_incidents.sql stores id, kind, scope, severity (info|warning|critical), title, object detail, state (open|acked|closed), first_seen, last_seen, count, acked_by, closed_at. Repeated opens for an active kind+scope increment count/last_seen. Ack accepts open; Close accepts open or acked; AutoClose closes active incidents for a detector kind/scope. Only a new critical incident requests notification. This manager is internal, not an HTTP endpoint. No ADMIN incident read/action/SSE route exists in the server or OpenAPI.
### Contract request for JOOXF-183
These are requested routes, not implemented routes:
- GET /admin/incidents?state=&severity=&q=&cursor=&limit= returns {items,next_cursor,server_time}; default limit 50, maximum 100. Search safe title, kind, area, summary only. Items include stored fields plus area, summary, detection, automatic_action, human_step, closed_by (admin|detector), closed_at, auto_closed derived from closed_by. Add/persist closure source because current storage cannot distinguish manual from detector close. Do not return raw detail.
- GET /admin/incidents/{id} returns the same safe fields and redacted evidence references only.
- POST /admin/incidents/{id}/ack records the authenticated admin and returns updated item. Only open is valid.
- POST /admin/incidents/{id}/close closes open or acked and returns closed_by=admin. AutoClose returns/streams closed_by=detector. Invalid transitions return conflict; unknown IDs return 404.
- GET /admin/incidents/stream is ADMIN-authenticated SSE with resumable event ID and incident.opened, incident.updated, incident.closed events. Each event carries a safe item and server timestamp. If unavailable/disconnected, poll every 30 seconds, preserve last-good rows, and mark stale; never turn a timeout into an empty list.
Use session identity, not client-supplied actor IDs. Enforce ADMIN and redact server-side. Severity, thresholds, and remediation must come from detector config/contracts; the UI must not infer them from kind. Apply stream updates idempotently by incident and event ID.

### Eleven flow cases
JOOXF-160 names these cases but does not define thresholds or detector-specific remediation. Until specified, show configured detector explanation and “No automatic remediation is defined”. Common automatic record behavior is open/deduplicate/count/last_seen; only a new critical incident requests notification.

| kind | Detection | Automatic action | Human step |
|---|---|---|---|
| quota_exhausted | Key quota at limit; reset/threshold from detector config. | Record/deduplicate; no key rotation or crawl restart. | Check safe key label/reset time in API quota; follow key policy. |
| quota_high_early | Projected quota exhaustion before reset; threshold from config. | Record/deduplicate; no automatic throttle specified. | Review quota trend/schedule; apply workload/key policy. |
| node_blocked | Heartbeat reports blocked; show safe reason. | Record/deduplicate; no failover implied. | Inspect node detail, blocked reason, heartbeat. |
| node_dead | Heartbeat missing past configured threshold. | Record/deduplicate; no restart/reassignment specified. | Verify host/node health; restore or replace through normal operations. |
| duplicate_egress_ip | Multiple nodes report the same egress IP. | Record/deduplicate; no automatic node change. | Compare safe node labels and arrange unique egress assignment. |
| key_revoked | Key source reports revoked; safe key label only. | Record/deduplicate; existing key validation rejects use. | Replace/reconnect through approved key flow; never reveal secret. |
| token_revoked | Provider reports revoked token; show provider/account label only. | Record/deduplicate; do not retry revoked token. | Re-authorize through provider consent flow. |
| queue_backlog | Oldest item exceeds configured threshold per plan/lane; length alone is not an incident. | Record/deduplicate; do not drop/replay queued work. | Inspect plan/lane, oldest age, dependencies. |
| dlq_growing | Dead-letter growth crosses configured condition. | Record/deduplicate; preserve items; no bulk replay/delete. | Review safe failures; choose reviewed retry or code fix. |
| download_failures | Download failures cross configured condition. | Record/deduplicate; preserve job; no automatic retry/delete. | Inspect job, node, safe error; retry only affected job. |
| sync_failures | Sync failures cross configured condition. | Record/deduplicate; preserve sync; no automatic retry/delete. | Inspect provider, target, safe error; retry only affected sync. |

The API supplies severity, area, title, and detector-authored safe copy. Do not infer severity/threshold from kind. Link to an existing admin view only with a stable safe reference; otherwise show the human step as text.

## Interactions
The acknowledge, resolve, retry, loading, and conflict behavior is specified in Actions below.

## Actions
- Acknowledge only open incidents. On success show returned actor/time. On conflict refresh and explain the state changed.
- Resolve open or acked with confirmation. Explain it changes incident state and does not stop the detector/workload. On success move to Resolved. Do not offer reopen; no operation exists.
- Auto-closed records are read-only and show source/time. Do not imply all resolved issues self-close.
- Retry repeats a failed read. Failed mutations retain row/state; refresh before retry. Never optimistically hide an incident.

## States
- Normal: rows, counts, filters, tabs, local timestamps with absolute UTC available, connection status.
- Loading: table skeleton, disabled counts/actions, announce “Loading issues”.
- Empty: unfiltered Open says “No issues” / “There are no open issues right now.” CTA “View resolved issues”. Filtered zero says “No issues match these filters” and offers “Clear filters”.
- Error: retain last-good data if present, say “Issues could not be loaded”, offer Retry. With no data show error, not empty.
- Partial/stale: retain last-good rows, show last successful update, mark counts stale, pause actions until item state is verified. Refresh before enabling.
- Forbidden: “Admin access required”, standard sign-in/access route, no incident data.
- Mutation pending/failure: disable action while pending. On failure keep row/detail, show inline error, refresh before retry.

### Ready
Show the incident rows, status and severity filters, sort, counts, and connection state.

### Loading
Show table skeletons, disable counts/actions, and announce the loading message.

### Empty
Show the no-issues copy and the View resolved issues call to action.

### Error
Keep any last-good data visible as stale; otherwise show the error panel and Retry.

## Live updates
Connect SSE after first authorized list response. Apply events by ID and show live only after connection succeeds. Resume with last event ID and bounded reconnect backoff. Poll every 30 seconds while disconnected. Successful refresh clears stale. Deduplicate replay; malformed events keep rows and trigger a refetch. Pause polling in hidden tabs and refresh when visible.

## Responsive
At 1440px use compact table and right detail drawer. At 390px use scrollable filters, stacked tabs, labeled cards with primary fields/actions, and full-screen detail. Primary content/actions do not require horizontal page scrolling.

## Accessibility
Use h1, labeled filters, semantic desktop table/mobile list, keyboard-operable selected tabs, visible focus, and focus return from detail to its row. Use shared confirmation dialog. Announce critical incidents and action results politely, not every count. Do not use color alone for severity/stale state; honor reduced motion.

## i18n
Values are EN / VI. Localize interpolated {count}/{time}.

| Key | EN / VI |
|---|---|
| issues.title | Issues / Sự cố |
| issues.description | Problems that need a person. / Sự cố cần người xử lý. |
| issues.tab.open | Open / Đang mở |
| issues.tab.acked | Acknowledged / Đã tiếp nhận |
| issues.tab.closed | Resolved / Đã xử lý |
| issues.filter.severity | Severity / Mức độ |
| issues.filter.all | All severities / Tất cả mức độ |
| issues.filter.search | Search issues / Tìm sự cố |
| issues.sort.lastSeen | Newest activity / Hoạt động mới nhất |
| issues.sort.oldest | Oldest first / Cũ nhất trước |
| issues.column.area | Area / Khu vực |
| issues.column.detail | Issue / Chi tiết |
| issues.column.firstSeen | First seen / Phát hiện lần đầu |
| issues.column.lastSeen | Last seen / Gần nhất |
| issues.column.count | Count / Số lần |
| issues.column.status | Status / Trạng thái |
| issues.action.acknowledge | Acknowledge / Tiếp nhận |
| issues.action.resolve | Resolve / Đánh dấu đã xử lý |
| issues.action.retry | Retry / Thử lại |
| issues.action.clearFilters | Clear filters / Xóa bộ lọc |
| issues.action.viewResolved | View resolved issues / Xem sự cố đã xử lý |
| issues.label.autoClosed | Auto-closed / Tự động đóng |
| issues.label.live | Live / Đang cập nhật |
| issues.label.stale | Stale · last updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| issues.state.loading | Loading issues / Đang tải sự cố |
| issues.empty.title | No issues / Không có sự cố |
| issues.empty.body | There are no open issues right now. / Hiện không có sự cố đang mở. |
| issues.empty.filtered | No issues match these filters / Không có sự cố phù hợp bộ lọc |
| issues.error.load | Issues could not be loaded / Không tải được danh sách sự cố |
| issues.error.forbidden | Admin access required / Cần quyền quản trị viên |
| issues.error.mutation | Issue could not be updated. Refresh and retry. / Không cập nhật được sự cố. Hãy tải lại rồi thử lại. |
| issues.resolve.title | Resolve this issue? / Đánh dấu sự cố này đã xử lý? |
| issues.resolve.body | This changes incident state; it does not stop the detector/workload. / Thao tác đổi trạng thái, không dừng bộ phát hiện hay tác vụ. |
| issues.severity.info | Info / Thông tin |
| issues.severity.warning | Warning / Cảnh báo |
| issues.severity.critical | Critical / Nghiêm trọng |

## Test hooks
The page root exposes data-testid="admin-issues-page"; all hooks are listed below.

## Test IDs
admin-issues-page, admin-issues-live-status, admin-issues-count-open, admin-issues-count-acked, admin-issues-count-closed, admin-issues-filter-severity, admin-issues-filter-search, admin-issues-sort, admin-issues-tabs, admin-issues-table, admin-issues-row-{id}, admin-issues-empty, admin-issues-loading, admin-issues-error, admin-issues-stale, admin-issues-forbidden, admin-issues-detail, admin-issues-ack-{id}, admin-issues-resolve-{id}, admin-issues-resolve-dialog, admin-issues-retry.

## Out of scope
Detector implementation/threshold policy (JOOXF-160 and detector tickets), infrastructure detector implementation (JOOXF-159), requested API/server implementation (JOOXF-183), notification delivery, bulk actions, reopen, detector configuration, and destructive workload/queue operations.
