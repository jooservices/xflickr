# Album detail

View id `user-album`, role USER, light gallery theme (dark optional). Route
`/albums/{album_id}`. Visual baseline: `vAlbum()` in the approved
[POC](../poc/xflickr-poc.html), USER role. Shared rules: [design
system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), and
[i18n](../i18n.md). Mockup: [user-album.html](../mockups/user-album.html).
Build ticket: JOOXF-174.

## Purpose

Show one Flickr album, its owner, Flickr photo count, XFlickr stored-photo
count, and the ordered photo records available to the signed-in user. Let the
user prepare a ZIP download or send the album to a connected cloud destination.

## Route and access

- `/albums/{album_id}`, USER role, active user session required. The server
  scopes the album and every photo to the session user; the browser sends no
  user id. `album_id` is the Flickr photoset id and is treated as an opaque
  string.
- Unauthenticated users follow the shell session-expiry flow and return to
  this route after sign-in. A 403 shows the shared access-denied state. An
  inaccessible or unknown album returns the same not-found state so its
  existence is not disclosed.
- Document title is `{album_title} · Albums · XFlickr`. On route entry, move
  focus to the page `h1`.

## Layout

Inside the USER shell main column (max width 1200px, 24px desktop padding,
16px mobile padding), render:

1. Breadcrumb link `Albums` to `/albums`.
2. Album heading with title, owner name, stored / Flickr total counts, and
   last-updated relative time when available.
3. Owner link to `/contacts/{owner_nsid}` and two actions: `Download album`
   and `Send to cloud`. Both open the existing POC `New download` modal with
   this album selected. `Send to cloud` preselects a connected destination.
4. A photo-led responsive grid in album position order. Each tile uses the
   best stored size URL, descriptive alt text from the photo title (or a
   localized untitled fallback), and opens the existing photo lightbox. Do not
   add selection, editing, sorting, or pagination controls.
5. When fewer photos are stored than the Flickr total, show a partial-store
   notice with both counts. Stored photos remain usable; unavailable or
   private photos are not represented by broken image placeholders.

The grid has four columns at 1100px and above, three at 800–1099px, two at
500–799px, and two compact columns below 500px (390px viewport). The shell
owns its navigation, notifications, theme and language controls.

## Data and API

Do not call Flickr from the browser. Existing OpenAPI operations are
`POST /downloads` and authenticated `GET /events`; there is no album-detail
read operation, cloud-destination operation, or cloud-aware download request
in the current contract/backend. JOOXF-174 must add the contract requests
below before implementing this view. These are requests, not current
endpoints.

| Element | Source | Fields used |
| --- | --- | --- |
| Album header and stored count | `GET /me/albums/{album_id}` **contract request** | `id`, `title`, `owner_nsid`, `owner_name`, `stored_count`, `total_count`, `updated_at` |
| Photo grid | Same detail response, `photos[]` | `id`, `title`, `alt_text`, `position`, `size_urls` (named size to URL map); include only photos visible to this user |
| Partial-store notice | Same detail response | `stored_count`, `total_count`; show when stored count is less than total count |
| Owner link | Same detail response | `owner_nsid`, `owner_name`; owner name falls back to NSID |
| New download modal estimate | `POST /downloads/estimate` **contract request**, already specified by the Downloads view | `kind: album`, `subject: album_id`, `size`, `destination`; response `item_count`, `estimated_bytes`, `missing_sizes`, `required_size_checks`, `server_time` |
| Create album download | Existing `POST /downloads`; its size and destination extension is specified by the Downloads view | `kind: album`, `subject: album_id`, selected `size`, and `destination: zip` |
| Send to cloud | `GET /cloud/providers` and `POST /downloads` **contract requests** | Provider `id`, `name`, `status`; list only `status=connected`. Create request uses `kind: album`, `subject: album_id`, `size`, and `destination` set to the connected `cloud_provider_id` |
| Live refresh | `GET /events` exists | Add `album.updated` event contract with `album_id`, optional `stored_count`, and `updated_at`; resume using `Last-Event-ID` |

Request `GET /me/albums/{album_id}` returns the current user's view of that
album. `total_count` is Flickr's photoset count; `stored_count` is the number
of eligible stored photo records in that album. `photos` contains those
eligible records in `position`, then `id`, ascending order. URLs come only
from recorded Flickr sizes and are selected server-side; the client never
constructs Flickr URLs. Return no credentials, private observations from
another user, or photos the current user cannot view. Return `updated_at` as
ISO 8601 UTC or null.

`GET /cloud/providers` returns cloud destinations as specified by the Downloads
view. The modal's `Deliver to` field includes ZIP link and connected providers
destinations. If none are connected, keep ZIP available and show an inline
`Connect a cloud destination` link to `/sync`; `Send to cloud` is disabled
with an explanatory label. Cloud requests use the existing download creation
operation with the requested contract extension; they do not upload directly
from this page.

Contract requests owned by this view: add the album detail read and the
`album.updated` event payload. The Downloads view owns the estimate and create
request extensions; the Downloads view also specifies `GET /cloud/providers`.
Do not modify OpenAPI in this design ticket.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Back to albums | Breadcrumb | Navigate to `/albums`. |
| Open owner | Owner link | Navigate to `/contacts/{owner_nsid}`. |
| Open photo | Photo tile | Open the existing accessible photo lightbox without changing album order. |
| Download album | Header button | Open `New download` modal with this album selected and ZIP link selected; confirm submits `POST /downloads`. |
| Send to cloud | Header button | Open the same modal with the first connected destination selected; the user can choose another connected destination and size before submitting. |
| Cancel modal | Modal Cancel or Close | Close without creating a download and return focus to the invoking button. |
| Retry | Error or stale notice button | Refetch the album detail; retain usable photos on background failure. |

The modal uses POC size choices: Original, Large 2048, Large 1600, Large 1024,
and Medium 800. It shows file count, estimated size, and pending `getSizes`
calls based on the response's size availability. This detail page does not
create a separate cloud upload flow.

## States

### Normal

Show the album header and photo grid. Show the stored-vs-total notice when
partial; omit it when all photos are stored. A successful album with zero
photos uses Empty. Mark the mockup state `data-state="ready"` for the shared
UI gate.

### Ready

Ready is the shared catalogue name for Normal; use the Normal layout.

### Loading

For the initial request, retain the shell and breadcrumb, show a title/owner
skeleton, count skeletons, and eight photo-card skeletons in the final grid
geometry. Set `aria-busy="true"`; hide skeletons from assistive technology.
During a refresh, keep existing header and photos visible with a small busy
status. Reduced-motion mode uses static skeletons.

### Empty

For an album response with no eligible photos, retain title and owner and show
“This album is empty”, “This album has no photos available to you yet.”, and a
“Browse albums” link to `/albums`. Do not offer download or cloud actions when
there are no eligible photos. This is a successful response, not an error.

### Error

For initial network or server failure, show “Could not load this album”,
“Check your connection, then try again.”, and `Retry`; do not show empty copy
or server error text. A 401 follows the shell session-expiry flow. A 404 uses
the same unavailable-album copy as an unknown id.

### Partial / stale

Partial means `stored_count < total_count`; keep the stored photos available
and display “{stored_count} of {total_count} photos stored”. If a background
refresh fails, retain usable content and show “Album may be out of date” with
`Retry`. A broken or expired size URL gets the neutral photo placeholder and
does not fail the whole album. A stale timestamp is rendered as “Updated
{relative_time}”.

### Forbidden

For 403, clear album data and show the shared access-denied title, text, and
home action. For an unknown or inaccessible album, show the same not-found
error and do not disclose whether it exists. Never retain stale album content
after access is denied.

## Live updates

This view is live. Reuse authenticated `GET /events`; on `album.updated` for
the current `album_id`, coalesce events for 500ms and refetch the detail.
Update counts, timestamp, and changed photo records without moving focus or
announcing every tile. Resume with `Last-Event-ID` as the shell specifies.

If SSE is unavailable or disconnected for 5 seconds, poll detail every 30
seconds while the document is visible. Pause polling while hidden and refresh
once when visible again. Do not poll faster than 30 seconds; stop on route
exit. Failed background refresh keeps usable data and enters Partial / stale.

## Responsive

Validate at 1440px desktop and 390px mobile. At 1440px the page uses the
1200px maximum content width, four photo columns, and inline actions. At
390px use two photo columns, 8px grid gaps, stacked/wrapping header actions,
and no horizontal page scrolling. Preserve image aspect ratio with
`object-fit: cover`; allow long titles and Vietnamese labels to wrap. All
interactive targets are at least 44px high. Also verify 200% zoom and EN/VI.

## Accessibility

- Use one `h1`; the owner is a semantic link and each photo tile is a native
  button with an accessible name that includes its title. Decorative images
  use empty alt text; missing images use a neutral placeholder, never a broken
  image glyph.
- Modal uses `role="dialog"`, `aria-modal="true"`, a heading label, trapped
  focus, Escape to close, and focus restoration. The selected destination and
  size are announced by native labelled controls.
- Initial loading is announced once; skeletons are hidden from assistive
  technology. Errors use `role="alert"`; live refresh does not move focus or
  announce every tile. State is never conveyed by color alone.
- Preserve visible 2px accent focus, 4.5:1 text contrast, reduced motion, and
  reflow at 320 CSS px.

## i18n

Provide EN (`en`) and VI (`vi`) values for every view-owned label, state,
message, action and accessible name. New download modal labels and actions use
the shared keys catalogued in the [Downloads view](user-downloads.md#i18n);
shell, common action, and lightbox copy remain owned by their specifications.
Album/photo titles and owner names come from Flickr and are not translated.

| Key | EN | VI |
| --- | --- | --- |
| `user.album.breadcrumb` | Albums | Album |
| `user.album.owner.link` | Open contact {owner_name} | Mở liên hệ {owner_name} |
| `user.album.counts` | {stored_count} of {total_count} photos stored | Đã lưu {stored_count}/{total_count} ảnh |
| `user.album.updated` | Updated {relative_time} | Cập nhật {relative_time} |
| `user.album.action.download` | Download album | Tải album xuống |
| `user.album.action.cloud` | Send to cloud | Gửi lên đám mây |
| `user.album.action.retry` | Retry | Thử lại |
| `user.album.loading.label` | Loading album | Đang tải album |
| `user.album.empty.title` | This album is empty | Album này trống |
| `user.album.empty.body` | This album has no photos available to you yet. | Album này hiện chưa có ảnh nào bạn có thể xem. |
| `user.album.empty.action` | Browse albums | Xem album |
| `user.album.error.title` | Could not load this album | Không thể tải album này |
| `user.album.error.body` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.album.error.unavailable` | This album is unavailable. | Album này không khả dụng. |
| `user.album.partial.notice` | {stored_count} of {total_count} photos stored | Đã lưu {stored_count}/{total_count} ảnh |
| `user.album.partial.stale` | Album may be out of date | Album có thể chưa được cập nhật |
| `user.album.cloud.connect` | Connect a cloud destination | Kết nối nơi lưu trữ đám mây |
| `user.album.photo.untitled` | Untitled photo | Ảnh chưa có tiêu đề |
| `user.album.photo.open` | Open photo {title} | Mở ảnh {title} |

## Test IDs

Stable E2E hooks:

- `data-testid="album-page"`
- `data-testid="album-breadcrumb"`
- `data-testid="album-header"`
- `data-testid="album-owner"`
- `data-testid="album-counts"`
- `data-testid="album-partial-notice"`
- `data-testid="album-download"`
- `data-testid="album-send-to-cloud"`
- `data-testid="album-grid"`
- `data-testid="album-photo"` (one per item; include id in `data-photo-id`)
- `data-testid="album-loading"`
- `data-testid="album-empty"`
- `data-testid="album-error"`
- `data-testid="album-stale"`
- `data-testid="album-retry"`
- `data-testid="album-download-modal"`
- `data-testid="album-download-submit"`

## Out of scope

Album editing, deleting, Flickr writes, search, sort, pagination, bulk photo
selection, new backend implementation, OpenAPI changes in this design ticket,
cloud destination management, download history, and admin views.

## Data

See Data and API.

## Interactions

See Actions.

## Test hooks

See Test IDs; every hook uses the `data-testid` attribute.
