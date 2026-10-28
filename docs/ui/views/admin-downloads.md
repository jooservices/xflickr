# Admin downloads

## Purpose

Give ADMIN an operational view of every user's download jobs, per-file checksum outcomes, transfer progress, and temporary storage pressure. Follow the approved POC. This view never exposes file links, payloads, secret material, or private Flickr metadata.

## Route and access

- Route `/admin/downloads` inside the ADMIN shell; dark control-room theme by default and the existing light theme as an option.
- Require an authenticated ADMIN session before loading. A 401 follows shell sign-in; a 403 shows the shared forbidden state and clears cached ADMIN data.
- The shell owns navigation, theme, language, and live control. This view owns filters, job drawer, refresh, and freshness.

## Layout

- Header shows “Downloads”, live/stale timestamp, and Refresh.
- Summary cards show running jobs, current throughput, temporary storage used/capacity and back-pressure, checksum failures in the last 24 hours, and completed-job success rate for the same window.
- Jobs panel has All, Active, Ready, and Finished filters; state, node, and time filters; a safe user/source search; and a paginated table: job, user label, scope/source label, node label, files/bytes progress, speed, temporary bytes, delivery, TTL or elapsed time, and state.
- Selecting a row opens a desktop drawer or mobile full-screen sheet. It shows safe job metadata and a paginated per-file table with file label, size, progress, speed, checksum result, attempts, duration, and state. Failed checks are prominent.
- Use generated file ordinals in the drawer. Never show original filenames, photo IDs, NSIDs, emails, private photo titles, ZIP URLs, signed URLs, blob keys, full checksum values, raw payloads, or raw errors.

## Data

Existing sources and new contract requests are distinguished in Data and API. This ticket documents the ADMIN screen only; it does not implement or edit API routes.

## Data and API

Paths are relative to `/api/v1`. Existing routes are user-scoped unless stated. Contract requests below belong to build ticket JOOXF-184 and are not implemented routes.

| Content                       | Source and fields                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Action                                                                                                                                                                                 |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Existing user download API    | `POST /downloads`, `GET /downloads/{id}/parts`, `POST /downloads/{id}/parts/{partNumber}/fetched`, and `POST /downloads/{id}/retry-failed` exist in OpenAPI and require USER access. They do not provide an ADMIN list or fleet-wide detail. `GET /events` is authenticated USER-scoped SSE.                                                                                                                                                                                                                                                                                                                                                                                                                                            | Do not call these routes from the ADMIN view or relax their ownership checks.                                                                                                          |
| Existing ops/storage evidence | `GET /admin/ops/status` reports crawl and queue counts, not download jobs. `BlobStore.Usage` and `BlobStore.Full` are internal ports; no ADMIN storage read model exists. Node heartbeat metrics are arbitrary JSON and are not a trusted download-storage API.                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Do not infer download totals or capacity from crawl counts or unvalidated node metrics.                                                                                                |
| Summary                       | Request ADMIN-only `GET /admin/downloads/summary`: `generated_at`, `window_start`, `window_end`, `job_counts_by_state`, `active_jobs`, `throughput_bytes_per_second`, `checksum_failures`, `completed_jobs`, `failed_jobs`, `success_rate`, and `storage{scope,used_bytes,capacity_bytes,backpressure_state,new_downloads_paused,observed_at,nodes[]}`. `scope` is `shared` or `per_node`; node entries contain only safe `node_label`, measured bytes/capacity and freshness. For `per_node`, list every measured node; for `shared`, return `nodes: []`. `backpressure_state` is `clear`, `warning`, `full`, or `unknown`. Do not sum incomparable shared and per-node measurements; unknown/stale capacity must not render as clear. | Refresh cards; show the storage scope and observation time. A full state explains that new downloads are paused; it does not expose a cleanup control.                                 |
| Job list                      | Request ADMIN-only `GET /admin/downloads?state=&node=&from=&to=&q=&cursor=&limit=` returning `items[{id,user_label,scope,source_label,size_label,delivery,state,created_at,started_at,completed_at,age_seconds,files_total,files_completed,files_failed,bytes_total,bytes_done,speed_bytes_per_second,temp_bytes,node_label,expires_at}],next_cursor,generated_at`. `id` is an opaque ADMIN job reference. `user_label` follows the approved ADMIN display policy; `source_label` is redacted and falls back to scope plus job ordinal for private sources. Search only safe labels and enums. Limit defaults to 50 and is capped at 100.                                                                                               | Filter and paginate server-side; open one job. Do not send a user identifier or search private source metadata.                                                                        |
| Job and file detail           | Request ADMIN-only `GET /admin/downloads/{id}?file_cursor=&file_limit=` for safe job fields and `files[{ordinal,file_label,size_bytes,bytes_done,speed_bytes_per_second,checksum_state,attempt,max_attempts,duration_ms,state,error_code,retryable}]`, plus `next_cursor` for files. `checksum_state` is `not_checked`, `verified`, `failed`, or `unavailable`; never return a digest. File labels are generated ordinals or approved redacted labels.                                                                                                                                                                                                                                                                                  | Drawer pages active files, failed files, the last eight completed files, and the next four queued files. “Show all files” pages the rest.                                              |
| Live updates                  | Existing `GET /events` carries USER-scoped download events and is not an ADMIN fleet stream. Request ADMIN-only `GET /admin/downloads/events` SSE with `Last-Event-ID` and safe `download.summary`, `download.job`, `download.file` events. Events use opaque ADMIN job references and aggregate file progress only; never include URLs, filenames, photo IDs, hashes, or raw errors.                                                                                                                                                                                                                                                                                                                                                   | Apply deduplicated updates by event ID. Poll summary and visible list/detail every 10 seconds after SSE disconnect; exponential backoff to 30 seconds and pause while hidden.          |
| Retry failed files            | Existing `POST /downloads/{id}/retry-failed` retries failed items only for the signed-in owner. Request ADMIN-only `POST /admin/downloads/{id}/retry-failed`; return `requeued`, current state, and `generated_at`. Require audit actor and idempotency; only retry eligible failed items in this one job.                                                                                                                                                                                                                                                                                                                                                                                                                              | Confirm the job and failed-file count. Disable while storage is full/unknown or the job is active. No retry-all-jobs action.                                                           |
| Cancel active job             | No cancellation route exists in OpenAPI. Request ADMIN-only `POST /admin/downloads/{id}/cancel` for queued, running, verifying, or syncing jobs only. Stop work for that one job, remove only its temporary assets after workers release them, keep the job/audit record, and return the updated state. Require audit actor and idempotency; reject terminal/non-cancelable jobs with 409.                                                                                                                                                                                                                                                                                                                                              | Require explicit confirmation that the user's active download will stop and its temporary files will be removed. Never cancel a ready/partial download or delete another job's assets. |

Job states are `queued`, `running`, `verifying`, `ready`, `partial`, `syncing`, `synced`, `expired`, `failed`, and `canceled`; file states are `queued`, `fetching`, `verifying`, `ready`, and `failed`. The UI maps these stable values explicitly and never infers them from queue internals.

ADMIN requests must enforce the role server-side and return the shared `{code,message}` error shape; do not reuse USER routes as an authorization shortcut. Timestamps are UTC. Define `success_rate` as successful terminal jobs divided by all successful or failed terminal jobs in the returned 24-hour window; return `null` when the denominator is zero. Audit every admin retry/cancel. These route and field requests are API contract work for JOOXF-184, not current capability.

## Interactions

- All/Active/Ready/Finished tabs map to explicit server state sets: Active is queued/running/verifying/syncing; Ready is ready/partial; Finished is synced/expired/failed/canceled. State, node, and time filters preserve each other; search debounces and Enter submits immediately.
- Open a row to see detail; closing returns focus to the row. Pagination preserves filters and sort. A load error never becomes an empty list or zero storage.
- Retry is enabled only for a server-confirmed terminal `partial` or `failed` job with eligible failed files and known available storage. Confirm the count; update only after the server response.
- Cancel is enabled only for a server-confirmed active job. Confirm the impact; do not optimistically remove the job. On conflict, refresh that job.
- The POC's per-job retry and cancel/delete actions are retained as contract requests with these limits. No bulk action, forced cleanup, storage purge, node drain, or worker control is in this view.

## Actions

| Action             | Source                                              | Result                                                                                    |
| ------------------ | --------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Filter jobs        | Job-list query                                      | Preserve filter values and update the server-paginated list.                              |
| Open job           | Requested `GET /admin/downloads/{id}`               | Show safe job details and paginated file states/checksum results.                         |
| Retry failed files | Requested `POST /admin/downloads/{id}/retry-failed` | Confirm one job; retry eligible failed files only; audit actor.                           |
| Cancel active job  | Requested `POST /admin/downloads/{id}/cancel`       | Confirm one active job; stop it and clean only its temporary assets; retain audit record. |
| Refresh            | Summary/list/detail reads                           | Refresh current sections without clearing filters.                                        |

## States

### Ready

Show current summary, storage/back-pressure, filtered jobs, and update time. State pills include text/icon as well as color. A `partial` job remains available and shows failed-file count and expiry.

### Loading

After 200ms show section-shaped skeletons and `aria-busy=true`; do not show fake counts or enable actions before eligibility is known.

### Empty

Show “No downloads yet” with “Download jobs will appear here when users start a download.” and a Refresh CTA. Empty filtered results explain the filters and offer Clear filters.

### Error

Show “Could not load downloads” and Retry. Keep successful sections and last-good values; never replace failed reads with zero or an empty list.

### Partial and stale

Mark only failed sections stale with their last update time and retain last-good values. Disable Retry/Cancel when eligibility or storage state is stale.

### Forbidden

A 401 follows the shell sign-in flow. A 403 shows “Admin access required”, uses the shared access route, and clears cached cross-user data.

### Storage full

Show observed used/capacity and explain that new downloads are paused. Keep job monitoring available; disable retries that would queue more storage work. Unknown or stale capacity is never shown as clear.

### Action pending or failed

Disable only the selected row while pending and announce the result. On failure retain its last known state and require a refresh before retrying.

## Live updates

Load one authorized summary and list snapshot before opening SSE. Mark Live only after connection succeeds. Resume with `Last-Event-ID` and bounded backoff. Apply events by event ID; a malformed event keeps last-good values and triggers a refetch. While disconnected, poll summary and visible records every 10 seconds (back off to 30 seconds); pause in hidden tabs and refresh on return. A success clears stale only for the updated section. The existing USER stream must never be used to construct cross-user ADMIN data.

## Responsive

At 1440px use summary cards, a compact jobs table, and right-side detail drawer. At 390px stack cards, wrap filters, use labeled job cards and a full-screen detail sheet with per-file rows. Keep byte counts, checksum states and action labels readable; no page-level horizontal scroll.

## Accessibility

Use one h1, labeled filters, semantic table/list, keyboard-operable tabs and buttons, and visible focus. Detail drawer traps focus and returns it to the selected row. Use a shared confirmation dialog for Retry and Cancel. Announce action outcomes and critical storage-state changes politely, not every byte tick. Pair status text/icons with color; honor reduced motion and `aria-busy` during loads.

## i18n

All values are EN / VI. Interpolate `{count}`, `{bytes}`, `{age}`, `{time}`. Keep error display localized by stable code; never render a server error message.

| Key                                     | EN / VI                                                                                                                                                        |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| admin.downloads.title                   | Downloads / Tải xuống                                                                                                                                          |
| admin.downloads.description             | Every user's download jobs and temporary storage health. / Tác vụ tải xuống của mọi người dùng và tình trạng bộ nhớ tạm.                                       |
| admin.downloads.metric.running          | Running / Đang chạy                                                                                                                                            |
| admin.downloads.metric.throughput       | Throughput / Tốc độ truyền                                                                                                                                     |
| admin.downloads.metric.storage          | Temporary storage / Bộ nhớ tạm                                                                                                                                 |
| admin.downloads.metric.checksumFailures | Checksum failures · 24h / Lỗi checksum · 24 giờ                                                                                                                |
| admin.downloads.metric.successRate      | Success rate · 24h / Tỷ lệ thành công · 24 giờ                                                                                                                 |
| admin.downloads.storage.warning         | Temporary storage is nearing its configured limit; new downloads are still running. / Bộ nhớ tạm đang gần chạm ngưỡng đã cấu hình; lượt tải mới vẫn hoạt động. |
| admin.downloads.storage.full            | New downloads are paused because temporary storage is full. / Tạm dừng tải mới vì bộ nhớ tạm đã đầy.                                                           |
| admin.downloads.storage.byScope         | Temporary storage by scope / Bộ nhớ tạm theo phạm vi                                                                                                           |
| admin.downloads.storage.observed        | Observed {time} ago / Được ghi nhận {time} trước                                                                                                               |
| admin.downloads.storage.usage           | {scope} · {used} used of {capacity} · {percent}% / {scope} · đã dùng {used} trên {capacity} · {percent}%                                                       |
| admin.downloads.storage.unknown         | Storage capacity is unavailable. / Chưa có thông tin dung lượng bộ nhớ.                                                                                        |
| admin.downloads.jobs.title              | Download jobs / Tác vụ tải xuống                                                                                                                               |
| admin.downloads.filter.all              | All / Tất cả                                                                                                                                                   |
| admin.downloads.filter.active           | Active / Đang hoạt động                                                                                                                                        |
| admin.downloads.filter.ready            | Ready / Sẵn sàng                                                                                                                                               |
| admin.downloads.filter.finished         | Finished / Đã kết thúc                                                                                                                                         |
| admin.downloads.filter.state            | State / Trạng thái                                                                                                                                             |
| admin.downloads.filter.window           | Time window / Khoảng thời gian                                                                                                                                 |
| admin.downloads.filter.node             | Node / Nút xử lý                                                                                                                                               |
| admin.downloads.filter.search           | Search safe labels / Tìm nhãn an toàn                                                                                                                          |
| admin.downloads.action.refresh          | Refresh / Làm mới                                                                                                                                              |
| admin.downloads.action.clearFilters     | Clear filters / Xóa bộ lọc                                                                                                                                     |
| admin.downloads.column.job              | Job / Mã tác vụ                                                                                                                                                |
| admin.downloads.column.user             | User / Người dùng                                                                                                                                              |
| admin.downloads.column.source           | Scope and source / Phạm vi và nguồn                                                                                                                            |
| admin.downloads.column.node             | Node / Nút xử lý                                                                                                                                               |
| admin.downloads.column.progress         | Files and bytes / Tệp và dung lượng                                                                                                                            |
| admin.downloads.column.speed            | Speed / Tốc độ                                                                                                                                                 |
| admin.downloads.column.temporary        | Temporary bytes / Dung lượng tạm                                                                                                                               |
| admin.downloads.column.delivery         | Delivery / Phương thức nhận                                                                                                                                    |
| admin.downloads.column.age              | Age / Thời gian                                                                                                                                                |
| admin.downloads.column.state            | State / Trạng thái                                                                                                                                             |
| admin.downloads.detail.title            | Download details / Chi tiết tải xuống                                                                                                                          |
| admin.downloads.detail.progress         | Progress / Tiến độ                                                                                                                                             |
| admin.downloads.detail.node             | Node / Nút xử lý                                                                                                                                               |
| admin.downloads.detail.temporary        | Temporary bytes / Dung lượng tạm                                                                                                                               |
| admin.downloads.detail.expiry           | Link expiry / Thời hạn liên kết                                                                                                                                |
| admin.downloads.detail.elapsed          | Elapsed / Thời gian đã trôi qua                                                                                                                                |
| admin.downloads.detail.files            | Files / Tệp                                                                                                                                                    |
| admin.downloads.detail.checksum         | Checksum / Kiểm tra checksum                                                                                                                                   |
| admin.downloads.checksum.verified       | Verified / Đã xác minh                                                                                                                                         |
| admin.downloads.checksum.failed         | Failed / Thất bại                                                                                                                                              |
| admin.downloads.checksum.pending        | Not checked / Chưa kiểm tra                                                                                                                                    |
| admin.downloads.files.ordinal           | File {ordinal} / Tệp {ordinal}                                                                                                                                 |
| admin.downloads.files.size              | Size / Kích thước                                                                                                                                              |
| admin.downloads.files.progress          | Progress / Tiến độ                                                                                                                                             |
| admin.downloads.files.speed             | Speed / Tốc độ                                                                                                                                                 |
| admin.downloads.files.attempts          | Attempts / Số lần thử                                                                                                                                          |
| admin.downloads.files.duration          | Duration / Thời lượng                                                                                                                                          |
| admin.downloads.files.state             | State / Trạng thái                                                                                                                                             |
| admin.downloads.checksum.unavailable    | Unavailable / Không khả dụng                                                                                                                                   |
| admin.downloads.status.queued           | Queued / Đang xếp hàng                                                                                                                                         |
| admin.downloads.status.running          | Running / Đang chạy                                                                                                                                            |
| admin.downloads.status.verifying        | Verifying / Đang xác minh                                                                                                                                      |
| admin.downloads.status.ready            | Ready / Sẵn sàng                                                                                                                                               |
| admin.downloads.status.partial          | Ready with errors / Sẵn sàng nhưng có lỗi                                                                                                                      |
| admin.downloads.status.syncing          | Syncing / Đang đồng bộ                                                                                                                                         |
| admin.downloads.status.synced           | Synced / Đã đồng bộ                                                                                                                                            |
| admin.downloads.status.expired          | Expired / Đã hết hạn                                                                                                                                           |
| admin.downloads.status.failed           | Failed / Thất bại                                                                                                                                              |
| admin.downloads.status.canceled         | Canceled / Đã hủy                                                                                                                                              |
| admin.downloads.delivery.zip            | ZIP / Tệp ZIP                                                                                                                                                  |
| admin.downloads.delivery.cloud          | Cloud / Đám mây                                                                                                                                                |
| admin.downloads.scope.contact           | Contact / Liên hệ                                                                                                                                              |
| admin.downloads.scope.album             | Album / Album                                                                                                                                                  |
| admin.downloads.scope.tray              | Tray / Khay                                                                                                                                                    |
| admin.downloads.files.showAll           | Show all files / Hiện tất cả tệp                                                                                                                               |
| admin.downloads.action.retryFailed      | Retry failed files / Thử lại tệp lỗi                                                                                                                           |
| admin.downloads.action.cancel           | Cancel job / Hủy tác vụ                                                                                                                                        |
| admin.downloads.action.cancelActive     | Cancel active job / Hủy tác vụ đang chạy                                                                                                                       |
| admin.downloads.action.confirmRetry     | Retry {count} failed files for this job? / Thử lại {count} tệp lỗi của tác vụ này?                                                                             |
| admin.downloads.action.confirmCancel    | Stop this active download and remove its temporary files? / Dừng lượt tải đang chạy này và xóa tệp tạm của nó?                                                 |
| admin.downloads.action.retryWarning     | This may use storage and worker capacity. / Thao tác này có thể dùng bộ nhớ và năng lực xử lý.                                                                 |
| admin.downloads.action.cancelWarning    | The user will lose this active download. / Người dùng sẽ mất lượt tải đang chạy này.                                                                           |
| admin.downloads.empty.title             | No downloads yet / Chưa có lượt tải nào                                                                                                                        |
| admin.downloads.empty.body              | Download jobs will appear here when users start a download. / Tác vụ sẽ xuất hiện khi người dùng bắt đầu tải xuống.                                            |
| admin.downloads.empty.filtered          | No jobs match these filters / Không có tác vụ phù hợp với bộ lọc                                                                                               |
| admin.downloads.state.loading           | Loading download jobs / Đang tải tác vụ xuống                                                                                                                  |
| admin.downloads.error.load              | Could not load downloads / Không tải được danh sách tải xuống                                                                                                  |
| admin.downloads.error.forbidden         | Admin access required / Cần quyền quản trị viên                                                                                                                |
| admin.downloads.error.action            | The action failed. Refresh the job and try again. / Thao tác thất bại. Hãy tải lại tác vụ rồi thử lại.                                                         |
| admin.downloads.label.live              | Live / Đang cập nhật                                                                                                                                           |
| admin.downloads.label.stale             | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time}                                                                                                      |

## Test hooks

The page root exposes `data-testid="admin-downloads-page"`; interactive hooks are listed below.

## Test IDs

admin-downloads-page, admin-downloads-live-status, admin-downloads-refresh, admin-downloads-summary, admin-downloads-storage, admin-downloads-storage-full, admin-downloads-jobs, admin-downloads-filter-state, admin-downloads-filter-node, admin-downloads-filter-search, admin-downloads-tab-all, admin-downloads-tab-active, admin-downloads-tab-ready, admin-downloads-tab-finished, admin-downloads-job-{id}, admin-downloads-job-detail, admin-downloads-files, admin-downloads-file-{ordinal}, admin-downloads-empty, admin-downloads-loading, admin-downloads-error, admin-downloads-stale, admin-downloads-forbidden, admin-downloads-retry-failed-{id}, admin-downloads-cancel-{id}, admin-downloads-confirmation, admin-downloads-clear-filters.

## Out of scope

Application/API/OpenAPI implementation (JOOXF-184), user download creation and source selection, ZIP-link delivery, cloud provider setup, editing storage limits, storage purges, forced cleanup, cross-job bulk actions, node management, and changes to worker concurrency or back-pressure policy.
