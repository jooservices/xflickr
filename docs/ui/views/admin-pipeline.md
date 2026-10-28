# Admin crawl pipeline

## Purpose
Give ADMIN one operational view of crawl lanes, plan backlog, discovery by depth, active work items and dead letters. Follow the approved POC. Use safe read models and one-item retry; never expose raw queue payloads or trigger bulk work.

## Route and access
- Route /admin/pipeline inside the ADMIN shell. Dark control-room is default; support the existing ADMIN light theme.
- Require an authenticated ADMIN session. Resolve role before loading data. A 401 follows shell sign-in; any 403 shows the standard forbidden state and clears cached ADMIN data.
- The shell owns navigation, role, theme, language and shared live control. This page owns its filters, job drawer, refresh and section freshness.

## Layout
- Header with live/stale time, Refresh, and Person/Sizes lane cards. Each shows queued, in flight, done this hour and blocked.
- Show Sizes priority, backlog grouped by plan/lane with oldest-item age, then the filterable Jobs table.
- Show discovery totals by depth with an expandable safe-label tree. Show Dead-letter queue separately with count, safe error summary, attempts and one-item Retry.
- A selected job opens a desktop drawer or mobile full-screen sheet with safe method/plan/lane, progress, attempts and retry times. Never show raw args, lock keys, contact NSIDs, email, key identifiers, tokens, private photo data, raw queue payloads or full error text.

## Data
Existing and requested sources are specified in Data and API below.

## Data and API
Paths are relative to /api/v1. Existing sources are labelled; contract requests for JOOXF-184 are not current routes.

| Content | Data source and fields | Action |
|---|---|---|
| Lane totals | Existing GET /admin/ops/status: generated_at, work_items, ready_jobs, river_jobs, photo_sizes_pending, photo_sizes_ready, photo_sizes_running, pending_items, oldest_pending_age_seconds, failed_work_items, dead_jobs, idle_reason. Existing GET /admin/queue: backlog_by_plan[{plan,lane,backlog_calls}], oldest_item_age_seconds, ready_queue_sizes[{queue,ready_jobs}]. These do not give done/hour or blocked counts by lane. Request GET /admin/pipeline/summary with lanes[{lane,queued,in_flight,done_last_hour,blocked}], generated_at, section freshness. | Lane card filters Jobs for that lane. Read-only; do not infer blocked/throughput from unlike totals. |
| Backlog by plan and age | Existing GET /admin/queue fields backlog_by_plan plan/lane/backlog_calls, oldest_item_age_seconds, ready_queue_sizes queue/ready_jobs. Shared work can be unassigned. | Filter Jobs by plan/lane; sort oldest first. |
| Sizes priority | Existing GET /admin/plans returns name, weight, max_depth_cap, max_contacts, ai_quota_per_day, zip_ttl_hours, resource_share; not the POC Sizes ordering. Request sizes_priority[{rank,rule,scope}] in GET /admin/pipeline/summary. | Informational only; no mutation control. |
| Discovery tree | Existing GET /me/crawl is user-scoped; GET /admin/users has no crawl tree. Request GET /admin/pipeline/discovery with depth_totals[{depth,contacts}], roots[{safe_label,depth,contact_count,children[]}], generated_at. Only approved display labels; no email, NSID, private photo data or contact notes. | Expand/collapse safe-label branches; no crawl start, pause or depth edit here. |
| Jobs table | No ADMIN job-list endpoint exists. Request GET /admin/pipeline/jobs?lane=&plan=&state=&q=&cursor=&limit= with items[{id,lane,plan_label,kind_label,state,subject_label,worker_label,progress_done,progress_total,attempt,max_attempts,created_at,age_seconds,next_retry_at,last_error_code}], next_cursor, generated_at. Limit defaults to 50, max 100. Search safe labels/enums only. | Open detail; retry only a confirmed eligible failed item. |
| Job drawer | No job detail endpoint exists. Request GET /admin/pipeline/jobs/{id} with safe row fields plus attempts[{number,started_at,duration_ms,result_code}], retry_policy_label, safe last_error_summary. Never serialize queue args or lock/payload. | Retry one eligible failed job; active/queued jobs are read-only. |
| Dead letters | Existing GET /admin/ops/status gives dead_jobs count only. Queue adapter emits job.dead to outbox with job args, queue, attempts and last error; no safe ADMIN list exists. Request GET /admin/pipeline/dead-letters?cursor=&limit= with items[{id (opaque dead-letter record ID),kind_label,queue_label,attempt_count,created_at,last_attempt_at,last_error_code,last_error_summary}], next_cursor, generated_at. Request GET /admin/pipeline/dead-letters/{id} for safe details and attempts. Redact server-side; do not expose River IDs or raw outbox data; browser never reads outbox. | Open detail and retry one River dead letter through its dedicated endpoint after confirmation. |
| Retry | Existing CLI xflickr requeue can requeue failed work items, including all when IDs are omitted. It is not an HTTP API and must not be exposed here. Request POST /admin/pipeline/jobs/{id}/retry for one failed work item and POST /admin/pipeline/dead-letters/{id}/retry for one dead-letter record using its opaque record ID. Keep this ID type separate from failed-work-item IDs; do not expose River IDs. Both require authenticated actor, audit event, idempotent result; reject active/non-retryable state with conflict. | Confirm one item; warn it can spend Flickr quota. No bulk retry. |
| Live updates | Existing scheduler publishes queue.state to outbox at startup and every minute. Existing GET /events is USER-scoped and is not an ADMIN pipeline stream. Request ADMIN-only GET /admin/pipeline/events SSE with resumable Last-Event-ID and safe pipeline.summary, pipeline.job, pipeline.dead_letter, pipeline.discovery events. | Shell Live pauses/resumes one stream. If unavailable, poll summary, queue, ops status, jobs and dead letters every 30 seconds. |

Existing plan/queue/ops endpoints are ADMIN-authorized with documented 401/403. Requested routes must enforce ADMIN and return the shared Error response. Timestamps are UTC, localized for display. Job state is a stable API enum mapped explicitly to queued, running, retry, failed or done; do not infer from a queue implementation detail. Record one-item retries in audit. Never expose fields that may contain credentials or personal data.

## Interactions
- Lane chips filter jobs to Person, Sizes or Both. Plan/state filters preserve each other; search debounces and Enter submits immediately.
- Selecting a row opens the drawer; closing returns focus to the row. Pagination keeps filters and sort.
- Retry is enabled only for a server-confirmed eligible failed work item or dead letter. Keep their endpoint and ID types separate. Confirm with safe ID and quota warning. Disable while pending; on conflict refresh that item. Never optimistically change state.
- The POC Cancel job is a demo action with no safe API contract. Omit it until separately specified and implemented; never send worker cancellation.
- Refresh reloads required sections and preserves filters/last-good values. Clear filters restores Both lanes and default sort.

## Actions
| Action | Source | Result |
|---|---|---|
| View Person/Sizes/Both | Lane filter | Filter jobs; do not change queue configuration. |
| View plan backlog | Existing GET /admin/queue plan/lane counts | Filter jobs by plan/lane. |
| Expand discovery | Requested admin discovery read model | Show safe labels/counts only. |
| Open job | Requested GET /admin/pipeline/jobs/{id} | Show safe detail and attempts. |
| Open dead letter | Requested GET /admin/pipeline/dead-letters/{id} | Show safe details and attempts; never expose River IDs or payloads. |
| Retry failed work item | Requested POST /admin/pipeline/jobs/{id}/retry | Confirm, enqueue one eligible item, audit actor, show returned state. |
| Retry dead letter | Requested POST /admin/pipeline/dead-letters/{id}/retry | Confirm one dead-letter record, audit actor, show returned state. |
| Refresh / Retry read | Existing/requested read endpoints | Reload; preserve last-good content on error. |

## States
- Normal: lane metrics, priority, backlog, jobs, discovery, dead-letter count/list, update time.
- Loading: section-shaped skeletons and aria-busy; no fake data or actions until eligibility is known.
- Empty: “No crawl work yet” / “The pipeline will appear when crawl work is scheduled.” CTA “Show both lanes”. Empty filtered jobs/dead letters explain scope and offer Clear filters.
- Error: “Pipeline data could not be loaded” and Retry. Keep successful sections; never treat API failure as zero backlog or an empty dead-letter list.
- Partial/stale: retain last-good section values, mark stale with timestamp, retry only failed source. Pause retries if eligibility is unverified.
- Forbidden: “Admin access required”; standard sign-in/access route and no cached data.
- Retry pending/failure: disable that row while pending; on failure retain item, show inline error and refresh before retry.

### Ready
Show successful sections with current counts and safe records.

### Loading
Show lane, table, tree and dead-letter skeletons on first load.

### Empty
Show no-work copy and Show both lanes or Clear filters.

### Error
Keep last-good sections where available; otherwise show error panel and Retry.

## Live updates
Open SSE after the first authorized snapshot. Apply updates by item/event ID and show Live only after connection succeeds. Resume with Last-Event-ID and bounded backoff. Poll existing and requested sources every 30 seconds while disconnected; success clears stale. Pause polling in hidden tabs and refresh on return. Malformed events keep content and trigger a refetch. The existing minute-level queue.state outbox is not an HTTP stream.

## Responsive
At 1440px use lane cards, compact backlog/job tables, tree panel and right drawer. At 390px stack panels, wrap filters, use labeled job cards and full-screen details. Avoid page-level horizontal scrolling; keep retry labels/counts readable.

## Accessibility
Use one h1, labeled filters, semantic tables/mobile lists, keyboard-operable lane tabs/tree disclosures and visible focus. Drawer traps focus and returns it to its row. Use the shared confirmation dialog for Retry. Announce retry result and critical queue changes politely, not every counter. Pair status text/icons with color and honor reduced motion.

## i18n
Values are EN / VI. Localize interpolated {count}, {age}, {time}.

| Key | EN / VI |
|---|---|
| pipeline.title | Crawl pipeline / Quy trình thu thập |
| pipeline.description | Crawl work, queue age, and safe retries. / Công việc thu thập, thời gian chờ và thử lại an toàn. |
| pipeline.lane.person | Person lane / Luồng người |
| pipeline.lane.sizes | Sizes lane / Luồng kích thước |
| pipeline.lane.both | Both lanes / Cả hai luồng |
| pipeline.metric.queued | Queued / Đang chờ |
| pipeline.metric.inFlight | In flight / Đang chạy |
| pipeline.metric.doneHour | Done this hour / Hoàn tất trong giờ |
| pipeline.metric.blocked | Blocked / Bị chặn |
| pipeline.priority.title | Sizes priority / Ưu tiên kích thước |
| pipeline.backlog.title | Backlog by plan / Công việc chờ theo gói |
| pipeline.backlog.oldest | Oldest item / Mục chờ lâu nhất |
| pipeline.backlog.plan | Plan / Gói |
| pipeline.backlog.lane | Lane / Luồng |
| pipeline.backlog.count | Backlog / Lượng chờ |
| pipeline.sort.oldest | Oldest first / Cũ nhất trước |
| pipeline.discovery.title | Discovery by depth / Khám phá theo độ sâu |
| pipeline.discovery.depth | Depth / Độ sâu |
| pipeline.discovery.contacts | Contacts / Liên hệ |
| pipeline.discovery.expand | Expand branch / Mở nhánh |
| pipeline.discovery.collapse | Collapse branch / Thu gọn nhánh |
| pipeline.jobs.title | Jobs / Tác vụ |
| pipeline.jobs.filter.plan | Plan / Gói |
| pipeline.jobs.filter.state | State / Trạng thái |
| pipeline.jobs.filter.search | Search jobs / Tìm tác vụ |
| pipeline.jobs.column.id | Job / Mã tác vụ |
| pipeline.jobs.column.lane | Lane / Luồng |
| pipeline.jobs.column.plan | Plan / Gói |
| pipeline.jobs.column.method | Work type / Loại công việc |
| pipeline.jobs.column.progress | Progress / Tiến độ |
| pipeline.jobs.column.attempt | Attempt / Lần thử |
| pipeline.jobs.column.nextRetry | Next retry / Lần thử tiếp |
| pipeline.jobs.column.state | State / Trạng thái |
| pipeline.jobs.column.action | Action / Thao tác |
| pipeline.jobDetail.title | Job details / Chi tiết tác vụ |
| pipeline.jobDetail.attempts | Attempt history / Lịch sử thử |
| pipeline.jobDetail.lastError | Last error / Lỗi gần nhất |
| pipeline.jobDetail.progress | Progress / Tiến độ |
| pipeline.deadLetters.title | Dead-letter queue / Hàng đợi lỗi cuối |
| pipeline.deadLetters.error | Last error / Lỗi gần nhất |
| pipeline.action.retry | Retry / Thử lại |
| pipeline.action.view | View details / Xem chi tiết |
| pipeline.action.confirmRetry | Retry this job? / Thử lại tác vụ này? |
| pipeline.action.retryWarning | This may use Flickr quota. / Thao tác này có thể dùng hạn mức Flickr. |
| pipeline.action.refresh | Refresh / Làm mới |
| pipeline.action.clearFilters | Clear filters / Xóa bộ lọc |
| pipeline.action.showBoth | Show both lanes / Hiện cả hai luồng |
| pipeline.label.live | Live / Đang cập nhật |
| pipeline.label.stale | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| pipeline.jobs.state.queued | Queued / Đang chờ |
| pipeline.jobs.state.running | Running / Đang chạy |
| pipeline.jobs.state.retry | Retry scheduled / Đã lên lịch thử lại |
| pipeline.jobs.state.failed | Failed / Thất bại |
| pipeline.jobs.state.done | Done / Hoàn tất |
| pipeline.state.loading | Loading pipeline / Đang tải quy trình |
| pipeline.empty.title | No crawl work yet / Chưa có công việc thu thập |
| pipeline.empty.body | The pipeline appears when crawl work is scheduled. / Quy trình sẽ xuất hiện khi có lịch thu thập. |
| pipeline.empty.filtered | No jobs match these filters / Không có tác vụ phù hợp bộ lọc |
| pipeline.error.load | Pipeline data could not be loaded / Không tải được dữ liệu quy trình |
| pipeline.error.forbidden | Admin access required / Cần quyền quản trị viên |
| pipeline.error.retry | The job could not be retried. Refresh and try again. / Không thử lại được tác vụ. Hãy tải lại rồi thử lại. |

## Test hooks
The page root exposes data-testid="admin-pipeline-page"; all interactive hooks are listed below.

## Test IDs
admin-pipeline-page, admin-pipeline-live-status, admin-pipeline-refresh, admin-pipeline-lane-person, admin-pipeline-lane-sizes, admin-pipeline-lane-both, admin-pipeline-backlog, admin-pipeline-priority, admin-pipeline-discovery, admin-pipeline-discovery-node-{id}, admin-pipeline-jobs, admin-pipeline-filter-plan, admin-pipeline-filter-state, admin-pipeline-filter-search, admin-pipeline-job-{id}, admin-pipeline-job-detail, admin-pipeline-empty, admin-pipeline-loading, admin-pipeline-error, admin-pipeline-stale, admin-pipeline-forbidden, admin-pipeline-dead-letters, admin-pipeline-retry-{id}, admin-pipeline-retry-dialog, admin-pipeline-clear-filters.

## Out of scope
Application/API/OpenAPI implementation (JOOXF-184), detector implementation, editing plans or lane priority, changing concurrency/backoff, pausing/resuming crawl, cancelling jobs, bulk retry/requeue, worker operations, and inspecting/modifying shared production queue data.
