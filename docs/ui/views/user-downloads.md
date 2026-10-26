# Downloads

View id `user-downloads`, USER role, light gallery theme (dark optional), route
`/downloads`. Baseline: `vDownloads()`, `dlRow()`, the new-download modal, and
the tray/job drawer patterns in the approved [POC](../poc/xflickr-poc.html).
Follow the [design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), and [i18n](../i18n.md).
Mockup: [user-downloads.html](../mockups/user-downloads.html). Build ticket:
JOOXF-177. The view renders in the [user shell](user-shell.md).

## Purpose

Create and monitor temporary photo downloads, choose ZIP or a connected cloud,
and manage photos selected for a later download. Download files are private,
temporary, verified before packaging/upload, and deleted at expiry or after
upload. A download never makes a permanent XFlickr archive.

## Route and access

- `/downloads`; authenticated USER sees only their downloads and tray. ADMIN
  opening this user route sees their own data; fleet operations belong to
  `/admin/downloads`. Unauthenticated requests return to
  `/login?reason=expired&return_to=/downloads`.
- Title `downloads.title`; focus moves to the `h1`. No user id is sent by the
  client; server scopes all records to the session.
- `?job=<id>` opens that user's job drawer. Remove the query when it closes.

## Layout

Main column max-width 1200px, 24px padding (16px mobile). Page head has title,
lead explaining stored sizes and temporary retention, and primary New download
button. Show a storage-full banner above the list when applicable.

- Download list: stable newest-first cards, each with source and scope, chosen
  size and destination, state pill, completed/total files and accessible
  progress bar. Show transferred/estimated bytes and MB/s while running. Ready
  or partial ZIP jobs show expiry countdown and Copy link; expired jobs show
  Prepare again; active jobs show Cancel. Partial jobs show failed-file count
  and open the job drawer. A completed cloud job says uploaded and temporary
  files deleted.
- New download modal: What (contact or album; tray opens with tray scope), then
  source selector, size chips (Original, Large 2048, 1600, 1024, Medium 800),
  Deliver to (ZIP link or connected cloud only), and estimate card for files,
  bytes, and missing sizes. Missing sizes warn how many `getSizes` calls are
  needed and that they consume the user's Flickr quota. Footer Cancel and Start.
- Download job drawer: right 480px drawer, mobile bottom sheet. Header has
  source, status and close. Summary has overall progress, bytes/speed, and
  expiry for ZIP. Per-file rows show photo, chosen size, state, progress,
  attempts, checksum result/failure, and Retry for retryable failures.
- Download tray panel: shell tray popover/panel lists selected photo thumbnail,
  title and owner, each with Remove; footer has count, estimate and Download.
  Download opens the same modal with tray source. Empty tray says no photos
  selected. Tray persists across reloads and devices.
- Components: page head, banner, card, pill, progress-bar, modal, chips, drawer,
  table/list rows, empty-block, skeleton, buttons, toast. No new component.

## Data and API

Paths below are relative to `/api/v1`; fields name JSON keys. Existing endpoints
are marked **exists**. A **contract request** is required before the build uses
it; this design ticket does not change OpenAPI.

| Content | Endpoint and fields |
| --- | --- |
| Create job | `POST /downloads` **exists**. Existing body `{kind, subject?, photo_ids?}` (`kind`: contact/album/tray; `photo_ids` for tray); response `id, kind, subject, status, created_at, expires_at, item_count, skipped`. **Contract request:** accept `size` (`original`, `large_2048`, `large_1600`, `large_1024`, `medium_800`) and `destination` (`zip` or connected `cloud_provider_id`); return `estimated_bytes`, `missing_sizes`, `zip_url` when ready, and `server_time`. Preserve 409 `temp_storage_full` and no-valid-key errors. |
| Estimate | `POST /downloads/estimate` **contract request**, called when What/source/size/destination changes and before creation. Accept the create selection fields plus `size` and `destination`; return `item_count, estimated_bytes, missing_sizes, required_size_checks, server_time`. `required_size_checks` counts `getSizes` calls and is used to explain quota cost. It must not create a job or reserve storage. |
| Download list | `GET /downloads` **contract request**; `items[]`: `id, kind, subject, status, size, destination, created_at, completed_at, expires_at, item_count, completed_count, failed_count, bytes_done, estimated_bytes, speed_bytes_per_second, zip_url`; `server_time`. Status: queued, running, verifying, ready, partial, expired, syncing, synced, failed, canceled. |
| Job detail and files | `GET /downloads/{id}` **contract request**; scoped job fields above plus `files[]`: `photo_id, title, thumb_url, size, status, bytes_done, bytes_total, attempt, max_attempts, checksum_status, error_code, retryable`. Per-file status: queued, fetching, verifying, ready, failed. |
| Cancel / redo / retry file | `DELETE /downloads/{id}` **contract request** (active job only); `POST /downloads/{id}/redo` **contract request** (expired/failed job creates a new job); `POST /downloads/{id}/files/{photo_id}/retry` **contract request** (failed retryable item). Return updated job; errors `job_not_cancelable`, `job_not_retryable`. |
| ZIP link | Returned `zip_url` is a short-lived, user-bound download URL from job list/detail after `ready` or `partial`; **contract request** must define its authenticated GET route and expiry behavior. Copy link uses Clipboard API and never exposes another user's file. |
| Sources and cloud | `GET /contacts` and `GET /albums` are **contract requests** for user-scoped source lists; use their existing view summary fields, with contact id/name and album id/title/owner. No private/unavailable source is selectable. Connected destinations use `GET /cloud/providers` **contract request**, `items[]: id, name, status`; show only `status=connected`. |
| Tray | `GET /tray`, `PUT /tray/items/{photo_id}`, `DELETE /tray/items/{photo_id}`, `DELETE /tray` are **contract requests** from the shell spec; GET returns `items[]: photo_id, title, owner_name, thumb_url, added_at`, `count, estimated_bytes`. Download submits selected `photo_ids` to existing POST. |
| Events | `GET /events` **exists**, authenticated SSE with `Last-Event-ID`; event frames have id and user scope. `download.progress` currently `{download_id, completed, total}`; `download.ready` currently `{download_id, status}`. **Contract request:** include byte/speed and per-file updates, plus `download.failed`, `download.expired`, `download.canceled` carrying `download_id` and enough updated job fields to reconcile. |
| Errors | Shared `Error`: `code, message, details`; localize by `code`, never render English `message`. Create returns 409 `temp_storage_full` or `no_valid_flickr_key`; missing-size quota is shown before submission. |

Stored Flickr size URLs are used; downloading those originals costs no further
Flickr calls. Missing size checks use the sizes lane and consume key quota.
Cloud sync deletes temporary files when complete or abandoned. ZIP parts/files
are deleted when their link expires. The expiry countdown uses
`expires_at - (server_time + elapsed)`.

## Actions

| Action | Effect |
| --- | --- |
| New download | Open modal; changing What/source/size/destination calls `POST /downloads/estimate`. Start posts the chosen fields and tray photo ids where relevant; on success close, show toast and insert job. |
| Copy link | Copy `zip_url`; announce copied, or offer selectable URL if clipboard access fails. |
| Cancel | Confirm, then `DELETE /downloads/{id}`; canceled job stops media work and removes temporary assets. |
| Prepare again | `POST /downloads/{id}/redo`; creates a new job with previous scope, size and destination. |
| Retry file | `POST /downloads/{id}/files/{photo_id}/retry`; updates only that failed file. |
| Tray add/remove/download | Add/remove photo through tray API; Download opens modal; remove item immediately and update count/estimate. |
| Retry load | Retry failed request only. SSE reconnect uses `Last-Event-ID`; after three failures poll `GET /downloads` and open drawer detail every 2s (backoff 2, 4, 8, max 30s), stopping while hidden. |

## States

### Ready

Normal list with mixed job states is valid. State pills have text and icon as
well as color. `partial` retains the ZIP link and exposes failures. Expired
ZIPs show `downloads.expired` and Prepare again; the expired URL is never
copyable. Failed checksum rows name the file and offer Retry only when
`retryable=true`. When the temporary store is full, show a persistent warning
that new downloads are paused; existing downloads remain visible and actions
that need storage are disabled until capacity returns.

### Loading

After 200ms without a response, keep the page head and show card/list skeletons,
`aria-busy=true`, and one `role=status` announcement. Timeout after 10s becomes
Error. Refresh retains current data with a title spinner.

### Empty

No jobs: download icon, “No downloads yet”, “Pick a contact or album and
choose Download.” and Browse contacts CTA. Empty tray separately says “Your
tray is empty” and invites adding photos from a gallery.

### Error

List failure replaces list with alert, “Could not load downloads”, a short
retry instruction, and Retry. Create failure stays in modal; `temp_storage_full`
explains capacity and that new requests are paused; no-key links to Keys;
network/5xx keeps modal values. Cancel/retry failure is a bad toast and leaves
job unchanged. Never show raw status, stack trace, or server message.

### Partial and stale

One list/detail/tray request failing marks only that block with inline error and
retry. Keep successfully loaded sections. On SSE outage or data older than 4s,
show Stale with last update time; polling is fallback. Pause polls in hidden tab,
resume with immediate fetch. Preserve last data while reconnecting.

### Forbidden

401 follows shell session-expiry flow and stops events/polling. 403 shows the
shared access-denied view without revealing job existence. A cross-user job id
returns the same not-found treatment as an unknown id. On a second
`csrf_rejected`, show action-failed toast.

## Live updates

Subscribe to authenticated `GET /events` while mounted. Apply events only to
the signed-in user's jobs. `download.progress` patches aggregate progress;
per-file progress events patch drawer rows; `download.ready` fetches final job
detail to receive URL/expiry; failed/expired/canceled events reconcile list and
drawer. Use event IDs and `Last-Event-ID` after reconnect. After three failed
SSE reconnects, poll list and open detail every 2s with exponential backoff to
30s; stop when hidden and immediately refresh on visibility return. A missing
event is repaired by polling. No live change reorders the list until a full
refresh; newly created job is inserted first.

## Responsive

At 900px cards become one column. Below 720px use 16px page gutters; card facts
wrap, actions become full-width rows, and file list becomes stacked rows. The
modal and job drawer become full-width bottom sheets with scrollable body and
sticky footer. At 390px, source/status and progress remain readable, with no
horizontal page scrolling. Vietnamese labels may wrap; no fixed-height copy.

## Accessibility

Use semantic heading order, labeled source/size/destination fields, native
buttons, visible 2px focus ring, and `aria-current` only for route navigation.
Progress exposes `role=progressbar`, value min/max/now and text equivalent
“n of total files”. Announce completed milestones and errors in a polite live
region; do not announce every byte tick. Modal/drawer has dialog name, traps
focus, closes on Escape, prevents background scrolling, and returns focus to
opener. Countdown is readable text; icons and color never carry state alone.
Honor reduced motion. Photo alt text uses title and owner; decorative thumbs
have empty alt.

## i18n

All user-facing copy has `downloads.*` keys; EN and VI values below cover every
view label, action, empty and error message. Dynamic values are named in braces.

| Key | EN | VI |
| --- | --- | --- |
| downloads.title | Downloads | Tệp tải xuống |
| downloads.lead | Temporary downloads from your Flickr photos. | Tải tạm thời ảnh Flickr của bạn. |
| downloads.new | New download | Tải xuống mới |
| downloads.scope | What | Nội dung |
| downloads.scope.contact | Contact | Người liên hệ |
| downloads.scope.album | Album | Album |
| downloads.scope.tray | Download tray | Khay tải xuống |
| downloads.scope.description | Choose what to download. | Chọn nội dung cần tải xuống. |
| downloads.source | Source | Nguồn |
| downloads.size | Size | Kích thước |
| downloads.size.original | Original | Bản gốc |
| downloads.size.large-2048 | Large 2048 | Lớn 2048 |
| downloads.size.large-1600 | Large 1600 | Lớn 1600 |
| downloads.size.large-1024 | Large 1024 | Lớn 1024 |
| downloads.size.medium-800 | Medium 800 | Trung bình 800 |
| downloads.destination | Deliver to | Gửi đến |
| downloads.destination.zip | ZIP link | Liên kết ZIP |
| downloads.destination.cloud | Connected cloud | Dịch vụ đám mây đã kết nối |
| downloads.destination.provider | {provider} | {provider} |
| downloads.modal.title | New download | Tải xuống mới |
| downloads.modal.start | Start download | Bắt đầu tải xuống |
| downloads.modal.cancel | Cancel | Hủy |
| downloads.modal.estimate-loading | Updating estimate… | Đang cập nhật ước tính… |
| downloads.details | Details | Chi tiết |
| downloads.drawer.title | Download details | Chi tiết tải xuống |
| downloads.drawer.close | Close download details | Đóng chi tiết tải xuống |
| downloads.drawer.job | Job details · {id} | Chi tiết lượt tải · {id} |
| downloads.drawer.progress | Per-file progress · {done} / {total} | Tiến độ từng tệp · {done}/{total} |
| downloads.drawer.retries | {count} retries | {count} lần thử lại |
| downloads.file.verified | Verified | Đã xác minh |
| downloads.file.retrying | Retrying · {percent}% | Đang thử lại · {percent}% |
| downloads.file.checksum-failed | Checksum failed | Mã kiểm tra không khớp |
| downloads.file.queued | Waiting | Đang chờ |
| downloads.file.fetching | Downloading | Đang tải |
| downloads.file.verifying | Verifying | Đang xác minh |
| downloads.file.ready | Complete | Hoàn tất |
| downloads.file.failed | Failed | Thất bại |
| downloads.tray.count | {count} photos in tray | {count} ảnh trong khay |
| downloads.tray.photo | {title} · {owner} | {title} · {owner} |
| downloads.files | Files | Tệp |
| downloads.bytes | Bytes | Dung lượng |
| downloads.bytes-estimated | Estimated bytes | Dung lượng ước tính |
| downloads.speed-label | Speed | Tốc độ |
| downloads.size-checks | {count} Flickr size checks will use quota. | {count} lần kiểm tra kích thước Flickr sẽ dùng hạn mức. |
| downloads.estimated-size | Estimated size | Dung lượng ước tính |
| downloads.checksum | Checksum | Mã kiểm tra |
| downloads.retries | Retries: {count} | Số lần thử lại: {count} |
| common.retry | Retry | Thử lại |
| downloads.estimate.files | Estimated files: {count} | Số tệp ước tính: {count} |
| downloads.estimate.bytes | Estimated size: {size} | Dung lượng ước tính: {size} |
| downloads.missing-sizes | {count} sizes are missing; checking them uses Flickr quota. | Thiếu kích thước của {count} ảnh; kiểm tra sẽ dùng hạn mức Flickr. |
| downloads.files-progress | {done} of {total} files | {done}/{total} tệp |
| downloads.bytes-progress | {done} of {total} | {done}/{total} |
| downloads.speed | {speed} MB/s | {speed} MB/giây |
| downloads.state.queued | Queued | Đang chờ |
| downloads.state.running | Downloading | Đang tải |
| downloads.state.verifying | Verifying | Đang xác minh |
| downloads.state.ready | Ready | Sẵn sàng |
| downloads.state.partial | Ready with failures | Sẵn sàng, có lỗi |
| downloads.state.expired | Expired | Đã hết hạn |
| downloads.state.syncing | Uploading | Đang tải lên |
| downloads.state.synced | Uploaded | Đã tải lên |
| downloads.state.failed | Failed | Thất bại |
| downloads.state.canceled | Canceled | Đã hủy |
| downloads.expires | Link expires in {time} | Liên kết hết hạn sau {time} |
| downloads.expired | This ZIP link expired. Prepare the download again. | Liên kết ZIP đã hết hạn. Hãy chuẩn bị lại. |
| downloads.copy | Copy link | Sao chép liên kết |
| downloads.copied | Download link copied. | Đã sao chép liên kết tải xuống. |
| downloads.copy-failed | Could not copy the link. Select and copy it manually. | Không thể sao chép liên kết. Hãy chọn và sao chép thủ công. |
| downloads.cancel-job | Cancel download | Hủy tải xuống |
| downloads.redo | Prepare again | Chuẩn bị lại |
| downloads.retry-file | Retry file | Thử lại tệp |
| downloads.cancel-confirm | Cancel this download? Temporary files will be removed. | Hủy lượt tải này? Các tệp tạm sẽ bị xóa. |
| downloads.cancelled | Download canceled. | Đã hủy tải xuống. |
| downloads.tray.title | Download tray | Khay tải xuống |
| downloads.tray.add | Add to download tray | Thêm vào khay tải xuống |
| downloads.tray.remove | Remove from tray | Xóa khỏi khay |
| downloads.tray.download | Download selected photos | Tải ảnh đã chọn |
| downloads.tray.empty | Your tray is empty. Add photos from a gallery. | Khay đang trống. Hãy thêm ảnh từ thư viện. |
| downloads.partial | {count} files failed. Open details to retry them. | {count} tệp bị lỗi. Mở chi tiết để thử lại. |
| downloads.checksum-failed | Checksum failed for {file}. | Mã kiểm tra của {file} không khớp. |
| downloads.storage-full | Temporary storage is full. New downloads are paused until space is available. | Bộ nhớ tạm đã đầy. Tải mới sẽ tạm dừng đến khi có chỗ trống. |
| downloads.empty.title | No downloads yet | Chưa có lượt tải xuống |
| downloads.empty.text | Pick a contact or album and choose Download. | Chọn người liên hệ hoặc album rồi chọn Tải xuống. |
| downloads.empty.cta | Browse contacts | Xem người liên hệ |
| downloads.loading | Loading downloads… | Đang tải danh sách… |
| downloads.error.title | Could not load downloads | Không thể tải danh sách |
| downloads.error.text | Check your connection and try again. | Kiểm tra kết nối rồi thử lại. |
| downloads.error.create | Could not start this download. Your choices are still here. | Không thể bắt đầu tải. Các lựa chọn của bạn vẫn được giữ. |
| downloads.error.no-key | Connect a valid Flickr key in Keys before downloading. | Hãy kết nối khóa Flickr hợp lệ trong mục Khóa trước khi tải. |
| downloads.error.storage-full | Temporary storage is full. Try again when space is available. | Bộ nhớ tạm đã đầy. Hãy thử lại khi có chỗ trống. |
| downloads.error.action | That action failed. The download has not changed. | Thao tác thất bại. Lượt tải chưa thay đổi. |
| downloads.error.forbidden.title | You do not have access to this download. | Bạn không có quyền truy cập lượt tải này. |
| downloads.error.not-found | We could not find that download. | Không tìm thấy lượt tải xuống này. |
| downloads.stale | Updated {time} ago | Cập nhật cách đây {time} |
| downloads.partial-error | This section could not load. Retry it. | Không thể tải phần này. Hãy thử lại. |
| common.cancel | Cancel | Hủy |
| downloads.start | Start download | Bắt đầu tải xuống |
| common.close | Close | Đóng |
| downloads.waiting | Waiting for your other download | Đang chờ lượt tải khác của bạn |
| downloads.checksum-ok | Checksum verified | Mã kiểm tra hợp lệ |
| downloads.failed-count | {count} files failed | {count} tệp bị lỗi |

## Test IDs

Stable E2E hooks: `downloads-page`, `downloads-new`, `downloads-list`,
`downloads-job-{id}`, `downloads-job-state-{id}`,
`downloads-progress-{id}`, `downloads-copy-{id}`, `downloads-cancel-{id}`,
`downloads-redo-{id}`, `downloads-job-drawer`, `downloads-file-{photo_id}`,
`downloads-file-retry-{photo_id}`, `downloads-tray`,
`downloads-tray-item-{photo_id}`, `downloads-tray-remove-{photo_id}`,
`downloads-tray-download`, `downloads-modal`, `downloads-source`,
`downloads-size-{value}`, `downloads-destination`, `downloads-estimate`,
`downloads-storage-full`, `downloads-empty`, `downloads-loading`,
`downloads-error`, `downloads-retry`.

## Test hooks

The build uses the `data-testid` values listed in [Test IDs](#test-ids). Add
hooks to the corresponding page, list, action, drawer, modal and state blocks.

## Data

The canonical endpoint and field map is [Data and API](#data-and-api).

## Interactions

The canonical action behavior is listed in [Actions](#actions).

## Out of scope

Admin fleet downloads, cloud-provider connection/setup, contact and album
management, API contract implementation, permanent storage, server-side ZIP
delivery implementation, and downloading private photos unavailable to the
signed-in Flickr account. Contract requests above belong to build tickets.
