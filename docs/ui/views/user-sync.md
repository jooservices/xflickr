# Cloud sync

View id `user-sync`, USER role, light gallery theme (dark optional), route
`/sync`. Baseline: `vSync()`, `upRow()`, and the upload-process drawer in the
approved [POC](../poc/xflickr-poc.html). Follow the [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), and [i18n](../i18n.md).
Mockup: [user-sync.html](../mockups/user-sync.html). Build ticket: JOOXF-178.
The view renders in the [user shell](user-shell.md).

## Purpose

Connect a user's cloud destinations and monitor photo uploads. Destinations
are Google Photos, Google Drive, OneDrive, Dropbox, and S3-compatible storage.
Cloud copies are the user's durable copies; XFlickr removes temporary source
files after an upload completes or is abandoned. Credentials and folder targets
belong to the signed-in user.

## Route and access

- `/sync`; authenticated USER sees only their provider connections and uploads.
  ADMIN at this route sees their own data; fleet operations belong to
  `/admin/sync`. Unauthenticated requests follow the shell session-expiry flow.
- Focus the page `h1` on route entry. No user id is sent by the client; server
  scopes every request to the session.
- `?upload=<id>` opens that user's upload-process drawer. Remove the query when
  the drawer closes. Unknown and cross-user IDs have the same not-found state.

## Layout

Use the USER light gallery theme by default and optional USER dark theme. Main
column max-width 1200px with 24px gutters (16px below 720px). The page heading
and lead explain that sync sends downloads to a connected destination and
temporary files are removed after upload.

- Provider cards for Google Photos, Google Drive, OneDrive, Dropbox, and
  S3-compatible storage show provider name, account name (or “Not connected”),
  target folder, connection/token state, and Connect, Disconnect, or Reconnect.
  Connected cards show the folder as editable text. Never display OAuth tokens,
  S3 secret keys, or full credential values.
- Uploads are newest first. Each card shows source, provider, folder, status,
  completed/total files, accessible overall progress, transferred/estimated
  bytes, speed while active, and retry count. Failed files and paused uploads
  open the process drawer.
- Upload-process drawer is 480px on desktop and a full-width bottom sheet on
  mobile. It shows overall progress and per-file rows with filename, status,
  progress, attempt count and Retry when retryable. Token-expired jobs show a
  reconnect action. Closing does not cancel an upload.
- Show a provider rate-limit slowdown notice with the provider and retry time.
  Do not display raw provider response bodies.
- If no provider is connected and no upload exists, use the “No cloud
  connected” empty state and a Connect provider call to action.
- Components: page head, card, pill, progress-bar, button, skeleton,
  empty-block, inline banner, drawer, list rows, toast. No new component.

## Data and API

Paths below are relative to `/api/v1`; fields name JSON keys. Existing
endpoints are marked **exists**. **Contract request** means the build must wait
for an OpenAPI contract; this design ticket does not change OpenAPI. Existing
`POST /downloads` accepts `{kind, subject?, photo_ids?}` only, so its
cloud-destination extension is a contract request.

| Content | Endpoint and fields |
| --- | --- |
| Provider connections | `GET /cloud/providers` **contract request**; `items[]`: `id, provider` (`google_photos`, `google_drive`, `onedrive`, `dropbox`, `s3`), `status` (`available`, `connected`, `token_expired`, `reconnect_required`), `account_name, folder, token_expires_at, updated_at`. Secret material is never returned. |
| Connect provider | `POST /cloud/providers/{provider}/connect` **contract request**; OAuth providers return `authorization_url` for a top-level redirect and a short-lived, one-use `state`; S3 accepts `account_name, bucket, region?, endpoint?, access_key_id, secret_access_key, folder`. Store S3 credentials encrypted; response returns the connection summary without secrets. |
| Disconnect / reconnect | `DELETE /cloud/providers/{id}` and `POST /cloud/providers/{id}/reconnect` **contract requests**. Reconnect returns `authorization_url, state` for OAuth; S3 reconnect uses the secure credential form. Disconnect pauses affected uploads and retains their retryable state. |
| Update folder | `PATCH /cloud/providers/{id}` **contract request**; request `{folder}`; response returns updated connection summary. Reject path traversal and provider-invalid folder names. |
| Upload list | `GET /cloud/uploads` **contract request**; `items[]`: `id, download_id, source_label, provider_id, folder, status, created_at, completed_at, total_files, completed_files, failed_files, bytes_done, bytes_total, bytes_per_second, retry_count, retry_after_seconds, error_code`; statuses `queued, running, rate_limited, token_expired, partial, completed, failed, canceled`. |
| Upload detail | `GET /cloud/uploads/{id}` **contract request**; summary fields above and `files[]`: `photo_id, filename, status, bytes_done, bytes_total, attempt, max_attempts, error_code, retryable`. Per-file status: `queued, uploading, completed, failed`. |
| Retry upload/file | `POST /cloud/uploads/{id}/retry` and `POST /cloud/uploads/{id}/files/{photo_id}/retry` **contract requests**; retry only retryable failures; response updated summary/detail. Errors include `upload_not_retryable`. |
| Start a cloud download | Existing `POST /downloads` **exists** creates a download only. **Contract request:** add `destination: {provider_id, folder?}` and have its response include `download_id` plus `upload_id` when handoff starts; define eligible connected destinations in the same contract. Do not call a separate upload-create route. |
| Live events | Authenticated `GET /events` with `Last-Event-ID` **exists**, currently streams `crawl.progress`, `download.progress`, `download.ready`. **Contract request:** add user-scoped `cloud.upload.progress` (`upload_id, completed_files, total_files, bytes_done, bytes_total, bytes_per_second`), `cloud.upload.file` (`upload_id, photo_id, status, bytes_done, bytes_total, retryable`), `cloud.upload.state` (`upload_id, status, error_code, retry_after_seconds`), and `cloud.provider.state` (`provider_id, status, token_expires_at`). Frames carry event IDs. |
| Errors | Shared `Error` envelope `code, message, details`; localize by `code`, never render provider body or server `message`. Contract errors: `provider_rate_limited` with `retry_after_seconds`, `provider_token_expired`, `provider_unavailable`, `cloud_credentials_invalid`. |

The POC's S3-compatible “static key” is a credential type, not a bearer-token
expiry countdown. Show “Configured” for a valid S3 connection and “Reconnect”
for invalid credentials. OAuth token state is “Connected”, “Expiring soon” when
`token_expires_at` is within 15 minutes, or “Sign-in expired”; refresh tokens
remain server side. The POC has no user-visible rate-limit notice; this spec
adds one because the required slowdown needs a clear recovery time. Contract
requests above belong to JOOXF-178 and later API work.

## Actions

| Action | Effect |
| --- | --- |
| Connect | For OAuth, request authorization URL and navigate to provider; return to `/sync` after callback. For S3, validate fields and submit credentials over the authenticated TLS API. Never put secrets in URL, logs, or browser storage. |
| Disconnect | Confirm, then delete connection. Active uploads pause and remain retryable; temporary files are retained only as long as required for retry, then deleted when abandoned. |
| Reconnect | OAuth starts a fresh authorization flow. S3 opens the credential form. On success refresh provider and upload data; resume eligible token-paused uploads. |
| Save folder | Patch folder and update card; an upload already running keeps its original target folder. |
| Open upload | Open drawer and set `?upload=<id>`. |
| Retry upload/file | Call the matching retry endpoint only when `retryable=true`; keep other files and jobs unchanged. |
| Start sync | On Downloads, choose a connected provider and folder in the existing New download flow; create through `POST /downloads` with the requested contract extension. This view does not create a second upload. |
| Retry load | Retry only the failed provider/list/detail request. |

## States

### Ready

Render provider cards and upload list. All five providers appear, including
available providers with Connect. Active upload rows update progress. Token
expiry pauses the upload and offers Reconnect. A provider rate limit pauses
that provider's upload and shows the retry time; retry resumes automatically
after `retry_after_seconds`, with manual Retry available for a failed file.
Statuses use text and icon as well as color.

### Loading

After 200ms without response, retain page heading and show card/list skeletons,
`aria-busy=true`, and one `role=status` announcement. A 10s timeout becomes
Error. On refresh retain existing data and show a small refresh indicator.

### Empty

When provider list is loaded and none is connected and there are no uploads,
show cloud icon, “No cloud connected”, “Connect a provider to send downloads
to your cloud.” and Connect provider action. When providers exist but uploads
do not, keep provider cards and show “No uploads yet” under Uploads; its CTA
links to Downloads.

### Error

Initial load failure shows “Could not load cloud sync”, “Check your connection
and try again.” and Retry. Provider/list/detail subrequest failure marks only
that section with inline retry; preserve other data. Connect, disconnect, folder
save, and retry failures keep form and current state and show a localized error.
Never show raw status, stack trace, credential, or server/provider message.

### Partial and stale

Show available blocks if another provider or upload request fails, with inline
retry for the failed block. On SSE outage or data older than 4 seconds, show
“Updated {time} ago” and preserve the last known values while polling. Newly
created uploads insert first; progress does not reorder existing rows.

### Forbidden

401 follows shell session-expiry flow and stops events/polling. 403 shows the
shared access-denied view without revealing a connection or upload's
existence. Unknown and cross-user IDs use the same not-found treatment. A
second `csrf_rejected` shows the action-failed message.

## Live updates

Reuse the shell's authenticated `GET /events` SSE connection; do not open a
second stream. Apply only events for the signed-in user's upload/provider IDs.
`cloud.upload.progress` patches aggregate progress, `cloud.upload.file` patches
drawer rows, and `cloud.upload.state` reconciles status, token expiry, provider
rate-limit notice and completion. `cloud.provider.state` updates its card.
Resume with event ID in `Last-Event-ID`. After three failed reconnects, poll
`GET /cloud/providers`, `GET /cloud/uploads`, and an open upload detail every
2 seconds, exponential backoff capped at 30 seconds. Stop polling when hidden;
fetch immediately when visible. Polling and SSE require the contract requests
listed above.

## Responsive

At 900px provider cards become two columns; below 720px they become one column,
with 16px page gutters. Cards and actions wrap without clipping. At 390px,
provider details stack, action buttons fill width, and uploads show stacked
source/status/progress facts with no horizontal page scrolling. Drawer becomes
a full-width bottom sheet with scrollable content and sticky header. EN/VI
labels may wrap; never truncate account, folder, or action names.

## Accessibility

Use semantic heading order, named provider sections, native buttons and labeled
account/folder/S3 credential fields. Progress exposes `role=progressbar`,
minimum, maximum, current value, and text equivalent “n of total files”.
Announce state changes, completed uploads and errors politely; do not announce
each byte tick. Drawer has a dialog name, traps focus, closes on Escape,
prevents background scrolling and returns focus to its opener. Focus ring is
visible at 2px. Status never relies on color alone. Honor reduced motion.
Credential inputs use password type and are cleared after successful save.

## i18n

All view copy uses `sync.*` keys. EN and VI values cover labels, empty/error
copy, actions, states, notices, and drawer content; placeholders are named in
braces.

| Key | EN | VI |
| --- | --- | --- |
| sync.title | Cloud sync | Đồng bộ đám mây |
| sync.lead | Send downloads to your cloud. Temporary files are removed after upload. | Gửi tệp tải xuống lên đám mây. Tệp tạm sẽ bị xóa sau khi tải lên. |
| sync.providers | Cloud providers | Nhà cung cấp đám mây |
| sync.provider.google-photos | Google Photos | Google Photos |
| sync.provider.google-drive | Google Drive | Google Drive |
| sync.provider.onedrive | OneDrive | OneDrive |
| sync.provider.dropbox | Dropbox | Dropbox |
| sync.provider.s3 | S3-compatible | Tương thích S3 |
| sync.account | Account | Tài khoản |
| sync.not-connected | Not connected | Chưa kết nối |
| sync.folder | Folder | Thư mục |
| sync.folder.save | Save folder | Lưu thư mục |
| sync.token.connected | Connected | Đã kết nối |
| sync.token.configured | Configured | Đã thiết lập |
| sync.token.expiring | Expiring soon | Sắp hết hạn |
| sync.token.expired | Sign-in expired | Phiên đăng nhập đã hết hạn |
| sync.token.invalid | Credentials need attention | Thông tin xác thực cần kiểm tra |
| sync.connect | Connect | Kết nối |
| sync.disconnect | Disconnect | Ngắt kết nối |
| sync.reconnect | Reconnect | Kết nối lại |
| sync.connect.title | Connect {provider} | Kết nối {provider} |
| sync.s3.account | Account name | Tên tài khoản |
| sync.s3.bucket | Bucket | Bucket |
| sync.s3.region | Region | Khu vực |
| sync.s3.endpoint | Endpoint (optional) | Endpoint (không bắt buộc) |
| sync.s3.access-key | Access key ID | Mã khóa truy cập |
| sync.s3.secret-key | Secret access key | Khóa bí mật truy cập |
| sync.s3.folder | Folder prefix | Tiền tố thư mục |
| sync.uploads | Uploads | Lượt tải lên |
| sync.upload.source | {source} | {source} |
| sync.upload.progress | {done} of {total} files | {done}/{total} tệp |
| sync.upload.bytes | {done} of {total} | {done}/{total} |
| sync.upload.speed | {speed} MB/s | {speed} MB/giây |
| sync.upload.retries | {count} retries | {count} lần thử lại |
| sync.upload.details | Upload details | Chi tiết tải lên |
| sync.upload.close | Close upload details | Đóng chi tiết tải lên |
| sync.upload.files | Files | Tệp |
| sync.upload.retry-file | Retry file | Thử lại tệp |
| sync.upload.retry | Retry upload | Thử lại lượt tải lên |
| sync.upload.queued | Queued | Đang chờ |
| sync.upload.running | Uploading | Đang tải lên |
| sync.upload.rate-limited | Slowed by provider | Nhà cung cấp đang giới hạn tốc độ |
| sync.upload.token-expired | Sign-in expired | Phiên đăng nhập đã hết hạn |
| sync.upload.partial | Completed with failures | Hoàn tất nhưng có lỗi |
| sync.upload.completed | Uploaded | Đã tải lên |
| sync.upload.failed | Failed | Thất bại |
| sync.upload.canceled | Canceled | Đã hủy |
| sync.rate-limit | {provider} is slowing uploads. Resumes in {time}. | {provider} đang làm chậm lượt tải lên. Tiếp tục sau {time}. |
| sync.no-provider.title | No cloud connected | Chưa kết nối đám mây |
| sync.no-provider.text | Connect a provider to send downloads to your cloud. | Kết nối nhà cung cấp để gửi tệp tải xuống lên đám mây. |
| sync.no-provider.cta | Connect provider | Kết nối nhà cung cấp |
| sync.empty.title | No uploads yet | Chưa có lượt tải lên |
| sync.empty.text | Choose a connected cloud destination when you prepare a download. | Chọn đích đến đám mây đã kết nối khi chuẩn bị tải xuống. |
| sync.empty.cta | View downloads | Xem tệp tải xuống |
| sync.loading | Loading cloud sync… | Đang tải đồng bộ đám mây… |
| sync.error.title | Could not load cloud sync | Không thể tải thông tin đồng bộ đám mây |
| sync.error.text | Check your connection and try again. | Kiểm tra kết nối rồi thử lại. |
| sync.error.action | That action failed. Your settings have not changed. | Thao tác thất bại. Cài đặt chưa thay đổi. |
| sync.error.credentials | These credentials could not be verified. Check them and try again. | Không thể xác minh thông tin này. Hãy kiểm tra rồi thử lại. |
| sync.error.forbidden | You do not have access to this upload. | Bạn không có quyền truy cập lượt tải lên này. |
| sync.stale | Updated {time} ago | Cập nhật cách đây {time} |
| sync.retry | Retry | Thử lại |
| sync.cancel | Cancel | Hủy |
| sync.close | Close | Đóng |

## Test IDs

Stable E2E hooks: `sync-page`, `sync-provider-list`,
`sync-provider-{provider}`, `sync-provider-state-{provider}`,
`sync-provider-account-{provider}`, `sync-provider-folder-{provider}`,
`sync-connect-{provider}`, `sync-disconnect-{provider}`,
`sync-reconnect-{provider}`, `sync-folder-save-{provider}`, `sync-s3-form`,
`sync-s3-account`, `sync-s3-bucket`, `sync-s3-access-key`,
`sync-s3-secret-key`, `sync-uploads`, `sync-upload-{id}`,
`sync-upload-state-{id}`, `sync-upload-progress-{id}`, `sync-upload-open-{id}`,
`sync-upload-drawer`, `sync-upload-file-{photo_id}`,
`sync-upload-file-retry-{photo_id}`, `sync-upload-retry-{id}`,
`sync-rate-limit-{provider}`, `sync-empty`, `sync-loading`, `sync-error`,
`sync-retry`.

## Test hooks

The build sets these hooks as `data-testid` attributes on their matching
provider cards, actions, upload rows, drawer, and state containers. The stable
values are listed in [Test IDs](#test-ids).

## Data

The canonical endpoint and field map is [Data and API](#data-and-api).

## Interactions

The canonical action behavior is listed in [Actions](#actions).

## Out of scope

Admin fleet sync at `/admin/sync`, provider administration, creating/editing
cloud folders remotely, cloud browsing, automatic Flickr crawling, permanent
XFlickr storage, implementation of the contract requests, and provider setup
wizard beyond the connection form described here. Contract requests belong to
JOOXF-178/API tickets.
