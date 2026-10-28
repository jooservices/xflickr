# Admin Cloud sync

## Purpose

Give ADMIN a fleet-wide, read-only view of cloud provider connections and upload health. Match the approved Cloud sync POC while exposing only the minimum data needed to operate the service. Connection and upload controls stay with the owning USER; this view does not change credentials, retry work, or cancel uploads.

## Route and access

- Route `/admin/sync` inside the ADMIN shell. Dark control-room theme is the default; the existing light theme remains available.
- Require an authenticated ADMIN session before requesting data. A 401 follows shell sign-in. A 403 shows the shared forbidden state and clears cached ADMIN data.
- Provider and upload data is fleet-wide only through the ADMIN contract requests below. Never call USER-scoped `/cloud/providers`, `/cloud/uploads`, or `GET /events` to build a fleet view.
- The shell owns navigation, theme, language and global live controls. This view owns filters, refresh, the connection and upload panels, upload drawer and freshness.

## Layout

- Header: “Cloud sync”, freshness/live status and Refresh.
- Summary cards: active uploads, uploads completed in the last 24 hours, provider error rate for the same window, and rate-limited providers.
- Provider health cards for Google Photos, Google Drive, OneDrive, Dropbox and S3-compatible storage: connected-user count, health, p95 API latency, attempt/failure counts and rate-limit state. A provider can be degraded even when it has no rate limit.
- Connected-connections panel lists a masked user label, provider, connection state and safe token state. A separate expired-token filter surfaces only connections requiring the USER to reconnect.
- Upload processes panel shows opaque upload reference, masked user label, provider, safe source label, per-file count and byte progress, throughput, retries, failed-file count and state. Filters cover state, provider and a bounded time range; search matches safe labels and opaque references only.
- Selecting an upload opens a right-side drawer on desktop and a full-screen sheet on mobile. It shows aggregate progress and file rows as generated ordinals (for example, “File 01”), with per-file byte progress, attempt count, state and localized error code.
- Do not show email, OAuth account name, folder or bucket path, original filename, Flickr title/NSID/photo ID, token expiry timestamp, secret material, provider response body, raw server error or signed URL. Use opaque references and masked user/source labels. Never infer user identity from a connection reference.

## Data

Data is ADMIN-only and read-only. Every value includes a source timestamp and a freshness state. The provider health window is the trailing 24 hours, in UTC; error rate is `failed_attempts / attempts * 100`, with both counts shown and a zero-attempt rate rendered as “—”. Unknown or stale data is never presented as healthy.

## Data and API

Paths are relative to `/api/v1`. Existing endpoints are identified separately; all ADMIN endpoints and event types below are **contract requests for JOOXF-184**, not implemented routes. Responses contain only the safe projection described here. Enforce ADMIN authorization on the server for every request and event.

| Content | Source, fields and action |
| --- | --- |
| Summary | **Contract request:** ADMIN-only `GET /admin/cloud-sync/summary`; `generated_at, window_start, window_end, active_uploads, completed_uploads, attempts, failed_attempts, error_rate_percent, rate_limited_providers`. Refresh updates the cards and freshness timestamp. |
| Provider health | **Contract request:** ADMIN-only `GET /admin/cloud-sync/providers`; `items[]: provider, connected_users, health_state (healthy, degraded, unavailable, unknown), p95_latency_ms, attempts, failed_attempts, error_rate_percent, rate_limit_state (clear, limited, unknown), retry_after_seconds, observed_at`. Open a provider filter to show its connections and uploads. Omit quota totals until a provider-independent, safely aggregated capacity contract exists. |
| Connections and expired-token list | **Contract request:** ADMIN-only `GET /admin/cloud-sync/connections?provider=&state=&cursor=&limit=`; `items[]: connection_ref, user_label, provider, state (connected, token_expired, reconnect_required, unavailable), token_state (valid, expiring_soon, expired, needs_attention, unknown), updated_at`. Filter to `token_expired` to list connections whose USER must reconnect. Open a read-only connection summary; no credential or reconnect action is exposed. |
| Upload list | **Contract request:** ADMIN-only `GET /admin/cloud-sync/uploads?state=&provider=&from=&to=&q=&cursor=&limit=`; `items[]: id, user_label, provider, source_label, status, created_at, total_files, completed_files, failed_files, bytes_done, bytes_total, bytes_per_second, retry_count, retry_after_seconds, error_code`. Open a row to view its detail. The server search operates only on safe labels and opaque references. |
| Upload detail | **Contract request:** ADMIN-only `GET /admin/cloud-sync/uploads/{id}`; summary fields above and `files[]: ordinal, status, bytes_done, bytes_total, attempt, max_attempts, error_code`. Render each file as a generated ordinal; do not return filenames, photo IDs, or payloads. |
| Existing USER sync APIs | `GET /cloud/providers`, `GET /cloud/uploads`, `GET /cloud/uploads/{id}`, the retry routes, and their events are USER-scoped contract requests owned by JOOXF-178. They are not ADMIN fleet endpoints and must not be reused or have ownership relaxed here. |
| Existing event stream | Authenticated `GET /events` exists and is USER-scoped. It currently carries crawl and download progress only; it cannot supply cross-user Cloud sync data. |
| ADMIN live stream | **Contract request for JOOXF-184:** ADMIN-only `GET /admin/cloud-sync/events`, resumable with `Last-Event-ID`. Events: `cloud.provider.health` (`provider, health_state, p95_latency_ms, attempts, failed_attempts, error_rate_percent, rate_limit_state, retry_after_seconds, observed_at`); `cloud.connection.state` (`connection_ref, provider, state, token_state, updated_at`); `cloud.upload.progress` (`upload_id, completed_files, total_files, failed_files, bytes_done, bytes_total, bytes_per_second`); `cloud.upload.file` (`upload_id, ordinal, status, bytes_done, bytes_total, attempt, max_attempts, error_code`); and `cloud.upload.state` (`upload_id, status, error_code, retry_after_seconds`). |
| Errors | Use the shared error envelope and stable codes `provider_rate_limited`, `provider_token_expired`, `provider_unavailable`, and `cloud_credentials_invalid`; map codes to local copy. Never display server messages or provider response bodies. |

## Actions

- Refresh reloads the summary, visible provider/connections/upload pages and the selected upload detail, preserving filters.
- Provider cards apply the matching provider filter. State, provider, bounded UTC time range and safe search update the upload list; connection state/provider filters update the connection panel. Pagination uses opaque cursors.
- Selecting an upload opens `?upload=<opaque-id>`; closing removes the query parameter. A missing or unauthorized reference displays the localized not-found/error state without leaking whether another user's upload exists.
- The expired-token list is informational. It directs operators to contact the masked owner through the established support process; only that USER can reconnect.
- No retry, cancel, disconnect, reconnect, quota edit, cleanup, bulk operation or credential inspection is available from this view. The POC provides no ADMIN mutation action; any future control requires a separate reviewed contract and authorization policy.

## Interactions

The upload drawer is read-only and deep-links through an opaque upload reference. Filter and pagination state can be shared in the URL, but no search value may include or resolve private account details. Keep the selected row and filter context when opening and closing the drawer.

## States

### Ready

Render summary, provider health, connection records, upload records and the selected drawer. Pair every status with text and show the observation time. Show “—” for unavailable latency or a zero-denominator rate; never substitute zero.

### Loading

On first load, show labeled skeletons for each panel and set `aria-busy="true"`. A panel refresh keeps its last good values visible with a small updating indicator.

### Empty

When there are no connections or uploads, show an explanatory empty state in the relevant panel and keep provider summary visible. When filters match no rows, offer Clear filters. Do not imply that no data exists if a request failed.

### Error

A failed initial summary or list request shows localized copy and Retry. A panel failure leaves other successful panels usable. For 401/403 follow the shell sign-in/forbidden behavior; never show a partial ADMIN view after 403.

### Partial and stale

Keep successful panels fresh if another panel or the event stream fails. Mark only affected panels “Stale · updated {time}”, retain their last good data, and offer Refresh. Back off polling when requests fail.

### Forbidden

For 403, clear ADMIN response data and drawer content, close the drawer and show the shared forbidden state. Do not expose whether a connection or upload exists.

## Live updates

Fetch an authorized snapshot before opening the ADMIN event stream. Show Live only after SSE connects; resume using `Last-Event-ID`. Apply events by ID and refetch a panel if an event is malformed or out of order. If SSE is unavailable, poll visible panels and the open detail every 10 seconds, backing off to 30 seconds on errors; pause while the tab is hidden and refresh on return. Mark a panel stale after 30 seconds without an observation. Do not use USER `GET /events` for fleet data.

## Responsive

At 1440px, show four summary cards, a provider card grid, a compact connection table and a two-column upload list/detail drawer. At 390px, stack the cards and panels; turn connection/upload rows into labeled cards; use full-width filter controls and a full-screen upload sheet. Keep status, progress and actions readable without page-level horizontal scrolling.

## Accessibility

Use one page-level h1, semantic headings and tables, explicitly labeled filters, keyboard-operable controls, visible focus and a focus-returning drawer. Announce panel refresh and state changes politely, not every progress tick. Set `aria-live="polite"` for freshness and `aria-busy` during loading. Pair status text with icons or labels, preserve contrast in both themes and honor reduced motion.

## i18n

Every visible label, state, empty/error message and action has an EN / VI key. Interpolate `{count}`, `{percent}`, `{time}`, `{done}`, and `{total}`. Localize by stable error code; never render an API message.

| Key | EN / VI |
| --- | --- |
| admin.sync.title | Cloud sync / Đồng bộ đám mây |
| admin.sync.description | Provider connections and upload health across XFlickr. / Kết nối nhà cung cấp và tình trạng tải lên trên toàn XFlickr. |
| admin.sync.action.refresh | Refresh / Làm mới |
| admin.sync.live | Live / Đang cập nhật |
| admin.sync.stale | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| admin.sync.metric.active | Active uploads / Lượt tải lên đang chạy |
| admin.sync.metric.completed | Completed · 24h / Hoàn tất · 24 giờ |
| admin.sync.metric.errorRate | Provider error rate · 24h / Tỷ lệ lỗi nhà cung cấp · 24 giờ |
| admin.sync.metric.rateLimited | Rate-limited providers / Nhà cung cấp bị giới hạn tốc độ |
| admin.sync.providers.title | Provider health / Tình trạng nhà cung cấp |
| admin.sync.provider.connections | {count} connected users / {count} người dùng đã kết nối |
| admin.sync.provider.latency | API p95 / Độ trễ API p95 |
| admin.sync.provider.attempts | Attempts / Lượt thử |
| admin.sync.provider.failures | Failed / Thất bại |
| admin.sync.provider.errorRate | Error rate / Tỷ lệ lỗi |
| admin.sync.provider.rateLimit | Rate limit / Giới hạn tốc độ |
| admin.sync.provider.retryAt | Retry in {time} / Thử lại sau {time} |
| admin.sync.provider.healthy | Healthy / Bình thường |
| admin.sync.provider.degraded | Degraded / Suy giảm |
| admin.sync.provider.unavailable | Unavailable / Không khả dụng |
| admin.sync.provider.unknown | Unknown / Chưa xác định |
| admin.sync.connections.title | Connected accounts / Tài khoản đã kết nối |
| admin.sync.connections.expired | Expired tokens · {count} / Token hết hạn · {count} |
| admin.sync.connection.user | User / Người dùng |
| admin.sync.connection.provider | Provider / Nhà cung cấp |
| admin.sync.connection.status | Connection / Kết nối |
| admin.sync.connection.updated | Updated / Cập nhật |
| admin.sync.connection.connected | Connected / Đã kết nối |
| admin.sync.connection.expiredState | Sign-in expired / Đăng nhập đã hết hạn |
| admin.sync.connection.reconnect | Reconnect required / Cần kết nối lại |
| admin.sync.connection.attention | Credentials need attention / Thông tin xác thực cần kiểm tra |
| admin.sync.uploads.title | Upload processes / Tiến trình tải lên |
| admin.sync.filter.state | Filter by state / Lọc theo trạng thái |
| admin.sync.filter.provider | Filter by provider / Lọc theo nhà cung cấp |
| admin.sync.filter.from | From date / Từ ngày |
| admin.sync.filter.to | To date / Đến ngày |
| admin.sync.filter.search | Search safe labels or reference / Tìm nhãn an toàn hoặc mã tham chiếu |
| admin.sync.filter.clear | Clear filters / Xóa bộ lọc |
| admin.sync.upload.reference | Upload / Lượt tải |
| admin.sync.upload.source | Source / Nguồn |
| admin.sync.upload.progress | Files / Tệp |
| admin.sync.upload.speed | Speed / Tốc độ |
| admin.sync.upload.retries | Retries / Số lần thử lại |
| admin.sync.upload.failed | Failed / Lỗi |
| admin.sync.upload.state | State / Trạng thái |
| admin.sync.upload.queued | Queued / Đang chờ |
| admin.sync.upload.running | Running / Đang chạy |
| admin.sync.upload.rateLimited | Rate limited / Bị giới hạn tốc độ |
| admin.sync.upload.partial | Partial / Một phần |
| admin.sync.upload.completed | Completed / Hoàn tất |
| admin.sync.upload.failedState | Failed / Thất bại |
| admin.sync.upload.canceled | Canceled / Đã hủy |
| admin.sync.drawer.title | Upload details / Chi tiết lượt tải lên |
| admin.sync.drawer.close | Close details / Đóng chi tiết |
| admin.sync.drawer.files | Files / Tệp |
| admin.sync.drawer.file | File {count} / Tệp {count} |
| admin.sync.drawer.attempt | Attempt / Lần thử |
| admin.sync.drawer.bytes | {done} of {total} / {done} trên {total} |
| admin.sync.empty.connections | No cloud connections / Chưa có kết nối đám mây |
| admin.sync.empty.uploads | No upload processes / Chưa có tiến trình tải lên |
| admin.sync.empty.filtered | No uploads match these filters / Không có lượt tải lên phù hợp bộ lọc |
| admin.sync.loading | Loading cloud sync data / Đang tải dữ liệu đồng bộ đám mây |
| admin.sync.error.load | Could not load cloud sync / Không tải được dữ liệu đồng bộ đám mây |
| admin.sync.error.provider | Provider status is unavailable / Không có trạng thái nhà cung cấp |
| admin.sync.error.upload | Upload details are unavailable / Không có chi tiết lượt tải lên |
| admin.sync.error.forbidden | Admin access required / Cần quyền quản trị viên |
| admin.sync.error.retry | The request failed. Refresh and try again. / Yêu cầu thất bại. Hãy làm mới rồi thử lại. |
| admin.sync.notFound | Upload not found / Không tìm thấy lượt tải lên |
| admin.sync.error.rateLimited | Provider is rate limited; retry in {time}. / Nhà cung cấp bị giới hạn tốc độ; thử lại sau {time}. |
| admin.sync.error.tokenExpired | The user must reconnect this provider. / Người dùng cần kết nối lại nhà cung cấp này. |
| admin.sync.error.providerUnavailable | Provider is temporarily unavailable. / Nhà cung cấp tạm thời không khả dụng. |
| admin.sync.error.credentials | The connection needs attention from its owner. / Chủ sở hữu cần kiểm tra kết nối. |

## Test IDs

`admin-sync-page, admin-sync-live-status, admin-sync-refresh, admin-sync-summary, admin-sync-metric-active, admin-sync-metric-error-rate, admin-sync-providers, admin-sync-provider-{provider}, admin-sync-connections, admin-sync-connection-{ref}, admin-sync-expired-tokens, admin-sync-filter-provider, admin-sync-filter-state, admin-sync-filter-from, admin-sync-filter-to, admin-sync-filter-search, admin-sync-clear-filters, admin-sync-uploads, admin-sync-upload-{id}, admin-sync-upload-detail, admin-sync-files, admin-sync-file-{ordinal}, admin-sync-loading, admin-sync-empty-connections, admin-sync-empty-uploads, admin-sync-empty-filtered, admin-sync-error, admin-sync-stale, admin-sync-forbidden`.

## Test hooks

The page root exposes `data-testid="admin-sync-page"`; every filter, provider card, upload row, drawer, file row and state panel has the stable hook listed above.

## Out of scope

Application code, OpenAPI edits, USER Cloud sync behavior, credential or OAuth management, provider setup, reconnect/disconnect, upload retry/cancel, quotas, storage cleanup, bulk actions, changes to provider rate-limit policy, and any change to other views.
