# Photo viewer (lightbox)

View id `user-lightbox`, role USER, light gallery theme (dark optional). This
full-screen overlay opens from a photo tile in a USER gallery and keeps the
underlying route. Visual baseline: `vLightbox()` and `.lb` in the approved
[POC](../poc/xflickr-poc.html). Follow the [design system](../design-system.md),
[components](../components.md), [accessibility](../a11y.md), and
[i18n](../i18n.md). Mockup:
[user-lightbox.html](../mockups/user-lightbox.html). Build ticket: JOOXF-171.

## Purpose

Let a signed-in user view a photo at a large size, move through the gallery's
ordered photo list, inspect its metadata, add it to the download tray, and
request a single-photo download. This is an overlay, not a new page or route.

## Route and access

- Opens over the current USER route when a photo tile is activated. Preserve
  the route, query, scroll position, and opener; do not add a standalone URL or
  browser-history entry. The current route determines the ordered photo list.
- Require an active user session. The server scopes all photo data, sizes,
  favorite names, and download requests to the session user; the browser sends
  no user id. A missing/expired session follows the shell sign-in flow.
- On open, set the document title to `{photo_title} · Photo viewer · XFlickr`
  and focus the close button. On close, restore focus to the photo tile. If it
  no longer exists, focus the underlying view's `h1`.

## Layout

Use the POC full-screen overlay: darkened stage on the left and a 400px
scrollable information panel on the right at 1440px. The stage centers the
photo without cropping, uses the POC hero shadow, and places previous/next
controls at its sides. The top row shows `{position} / {count}`, Add to tray,
Download photo, and Close. The panel order is title and owner link; AI caption;
sizes; EXIF; tags; Flickr page link; then favorited-by. Omit empty metadata
sections, but keep their loading placeholders while unresolved. The stage and
panel fill the viewport and account for safe-area insets.

Below 900px, stack stage above the panel; constrain the photo to 50vh and let
the page scroll. At 390px controls remain at least 44px, the stage and panel
stack vertically, and there is no horizontal scrolling. Follow the same
geometry in both USER light and dark themes.

## Data and API

Paths are relative to `/api/v1`. Do not call Flickr from the browser. Current
contracts include `POST /downloads` and authenticated SSE `GET /events`; no
photo detail or tray mutation read API exists. The photo detail read below is a
contract request for JOOXF-171, not an existing endpoint. Never return a Flickr
credential or a URL the user cannot access.

| Content | Source and fields |
| --- | --- |
| Stage and position | `GET /me/photos/{photo_id}` **contract request**: `id`, `title`, `owner_nsid`, `owner_name`, `position`, `total`, `display_url`, `alt_text`, `visibility`, `removed`. `position` and `total` come from the invoking gallery; preserve its list order when stepping. `display_url` is a server-selected recorded size URL. |
| Owner link | Same response: `owner_nsid`, `owner_name`; link to `/contacts/{owner_nsid}`. |
| Sizes | Same response: `sizes[]: label, width, height, url, source, confirmed_at`. `source` is `provisional` or `confirmed`; provisional entries are the photo-list sizes, confirmed entries are from `flickr.photos.getSizes`. `confirmed_at` is null for provisional entries. Dimensions are pixels. |
| EXIF | Same response: `exif[]: label, value`; server provides camera, lens, exposure, ISO, taken date, and original dimensions only when known. Omit absent values. |
| AI caption and tags | Same response: `ai_caption: {text, model, status}` (`status`: queued, ready, failed); `tags[]: id, name`. Caption model details are helper text; tags link to the existing search route with the tag as the query. |
| Flickr page | Same response: `flickr_url`; render a descriptive external link and open a new tab with `rel="noopener noreferrer"`. |
| Favorited-by | Same response: `favorited_by[]: nsid, display_name, avatar_url`; show up to six names and a localized remaining count. Do not expose people outside this user's visible record. |
| Add or remove from tray | `PUT /tray/items/{photo_id}` and `DELETE /tray/items/{photo_id}` are contract requests owned by the shell/Downloads specs. Use the response to update the shell tray count. Neither operation is currently in OpenAPI. |
| Download one photo | Existing `POST /downloads` supports `{kind: "tray", photo_ids: [photo_id]}`. Downloads owns the contract request to add `size` and `destination`; submit one photo id and selected size with `destination: "zip"`. Use the same size choices as Downloads. Do not create a separate download flow. |
| Prioritize size lookup | `POST /me/photos/{photo_id}/sizes/priority` **contract request**; response `202` means enqueue or prioritize one sizes-lane lookup using the user's interactive reserve. Do not make a Flickr call from this view. |
| Live change | Existing `GET /events` supports user-scoped SSE and `Last-Event-ID`. Add `photo.updated` (`photo_id`, `updated_at`) and `photo.removed` (`photo_id`, `reason`) event contracts; these are contract requests, not current event types. |

`GET /me/photos/{photo_id}` returns only records visible to the signed-in user.
For unknown and inaccessible records use the same not-found response. If Flickr
has removed a previously stored photo, return a tombstone with `removed: true`
and no display or size URLs. A private photo that is not currently visible is
403 and its existence and metadata must not be disclosed. The server may use
stored data and its permitted Flickr credentials; the browser only receives
the user-scoped response.

The initial list context (ordered IDs, current index, and origin tile) is
provided by the invoking gallery. Fetch detail once on open and when the
current ID changes; do not fetch adjacent photo details until navigation.
Single-photo download uses the existing `POST /downloads` operation with the
Downloads-owned size extension. The top Download photo action selects Original;
a size-row download button selects that row's size and submits directly. A
create failure retains the viewer and shows an inline error. All contract
requests above are listed for their owning build or shared-shell ticket; this
design ticket does not change OpenAPI.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Previous / next | ArrowLeft / ArrowRight or side buttons | Show the adjacent photo in the invoking gallery order; wrap at either end. Update detail and position without moving focus from a side button. |
| Close | Escape or Close button | Close the overlay, restore the underlying route and scroll, then return focus to the opener. Escape closes only the topmost overlay. |
| Add to tray | Top action | Toggle current photo in the tray through `PUT` or `DELETE /tray/items/{photo_id}`; update selected state and shell count. |
| Download photo | Top action or a size row's download button | Submit the current photo and selected size directly to `POST /downloads` with ZIP destination. The top action defaults to Original. On failure retain the viewer and show an inline error. |
| Open owner | Owner link | Navigate to `/contacts/{owner_nsid}` and close the viewer. |
| Open Flickr page | Flickr page link | Open `flickr_url` in a new tab. |
| Search tag | Tag button | Navigate to existing search with the tag query and close the viewer. |
| Retry | Error or stale notice | Refetch the current photo detail; retain usable stage content on background failure. |

## States

### Normal

Show stage and available panel fields, with position and action controls. The
mockup's `data-state="ready"` is the normal state. Hide sections with no known
content after loading completes. A successful record with no EXIF, tags,
caption, or favorites remains usable.

### Ready

Ready is the shared catalogue name for Normal; use the Normal layout.

### Loading

Loading uses the skeleton state.

On first open or photo change, retain the overlay and position. Use a neutral
stage placeholder and line skeletons in the panel, set `aria-busy="true"`, and
announce “Loading photo” once. Hide skeletons from assistive technology. When
moving between photos, keep the previous image dimmed until the new image
loads. Reduced motion uses static skeletons.

### Empty

If the invoking gallery has no photo at the requested index, close the overlay
and return to that gallery's own empty state. Do not show a blank viewer or
invent an alternate photo. This guard state has no separate retry action.

### Error

For initial network or server failure, show “Could not load this photo”,
“Check your connection, then try again.”, and Retry in the stage; keep Close
available. For a stage image load error after metadata succeeds, retain panel
data and show “Photo preview unavailable” with Retry. Never render raw server
messages or a broken-image glyph. A failed size-list request keeps provisional
sizes and shows a retryable partial notice.

### Partial / stale

Partial means some optional content failed or sizes are provisional. Keep
available content and label each size as provisional or confirmed. If SSE
disconnects or a background refresh fails, preserve the last usable photo and
show “Showing saved details” with last-updated time and Retry. Reconcile updates
without moving focus or replacing the current photo unexpectedly.

### Forbidden

A 403 clears photo data and shows the shared access-denied copy and home action.
Never retain the previous photo after access is denied. The 404/not-found copy
is the same for unknown and inaccessible IDs. A 401 follows shell expiry and
stops live updates.

### Sizes not yet fetched

Show any provisional sizes with a Provisional badge and dimensions, explain
that confirmed sizes are waiting for the sizes lane, and show “Fetch sizes
first”. Activating it calls the size-priority contract request and announces
that the lookup is queued; do not claim the size has already been fetched. If
no provisional sizes exist, show “Sizes are not available yet”. A later
confirmed response replaces matching provisional entries and marks its source
Confirmed; it does not duplicate a size row.

### Image load error

Keep metadata and controls available. Replace only the stage image with a
neutral placeholder, descriptive message, and Retry. Retry the recorded
`display_url` once on activation; a second failure returns to this state.

### Removed on Flickr (tombstone)

For `removed: true` or `photo.removed`, hide the image and all size/download
actions. Show “This photo is no longer available on Flickr” while preserving
the title only if returned in the user-scoped tombstone. Previous/next and
Close remain available. Do not retry a removed photo or expose its old URL.

### Private / not visible

For 403, clear every field and show “This photo is private or unavailable”.
Do not show the owner, title, favorite names, cached image, or whether the ID
exists. Previous and next remain available using the invoking gallery's
already-authorized ordered IDs; if another ID is also forbidden, show the same
state for that ID.

## Live updates

This overlay is live while open. Subscribe to authenticated `GET /events` and
resume with `Last-Event-ID`. For `photo.updated` matching the current ID,
coalesce changes for 500ms and refetch detail; update text and sizes in place.
For `photo.removed`, switch to the tombstone state immediately. Ignore events
for other IDs. Do not announce routine metadata changes or move focus.

If SSE is unavailable or disconnected for 5 seconds, poll current detail every
30 seconds while visible. Pause polling when the document is hidden and refresh
once when visible again. Stop on close or route exit. Failed background refresh
keeps content and enters Partial / stale. The gallery owns updates to adjacent
list membership and order.

## Responsive

Validate at 1440px desktop and 390px mobile, in both themes. At 1440px use a
flexible stage plus 400px panel. Below 900px stack the stage and panel; at
390px the image is at most 50vh and the details scroll below. Controls remain
visible, wrap without overlap, and meet 44px targets. Honor safe areas, long
Vietnamese labels, 200% zoom, and 320 CSS px reflow. Preserve image aspect ratio
with `object-fit: contain`; no horizontal page scrolling.

## Accessibility

- Use `role="dialog"`, `aria-modal="true"`, and a name from the photo title;
  make the background inert and trap focus. Initial focus is Close. On close,
  return focus to the opener or the underlying `h1` fallback.
- Stage image alt is the photo title or caption. Side buttons are native buttons
  named Previous photo and Next photo; ArrowLeft / ArrowRight work while focus
  is inside the viewer. Escape closes it. Do not intercept keys in text fields.
- Label tray and download actions with text or localized accessible names.
  Provisional/confirmed state and dimensions are text, not color alone.
- Loading uses one status announcement; blocking errors use `role="alert"`.
  Live updates do not move focus or announce each field. Show a visible 2px
  focus ring, meet 4.5:1 text contrast, and honor reduced motion.

## i18n

Every user-facing label, state, empty/error message, action, and accessible name
has EN and VI copy below. Flickr titles, names, tags, EXIF values, and captions
are source data and are not translated. Shared shell and download-modal copy
remain owned by those specifications.

| Key | EN | VI |
| --- | --- | --- |
| `user.lightbox.title` | Photo viewer | Trình xem ảnh |
| `user.lightbox.position` | Photo {position} of {count} | Ảnh {position}/{count} |
| `user.lightbox.close` | Close photo viewer | Đóng trình xem ảnh |
| `user.lightbox.previous` | Previous photo | Ảnh trước |
| `user.lightbox.next` | Next photo | Ảnh tiếp theo |
| `user.lightbox.owner` | Open contact {owner_name} | Mở liên hệ {owner_name} |
| `user.lightbox.tray.add` | Add to download tray | Thêm vào khay tải xuống |
| `user.lightbox.tray.added` | Added to tray | Đã thêm vào khay |
| `user.lightbox.download` | Download photo | Tải ảnh xuống |
| `user.lightbox.loading.label` | Loading photo | Đang tải ảnh |
| `user.lightbox.empty.title` | No photo to show | Không có ảnh để hiển thị |
| `user.lightbox.empty.body` | Return to the gallery and choose a photo. | Quay lại thư viện và chọn ảnh. |
| `user.lightbox.empty.action` | Return to gallery | Quay lại thư viện |
| `user.lightbox.error.title` | Could not load this photo | Không thể tải ảnh này |
| `user.lightbox.error.body` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.lightbox.error.preview` | Photo preview unavailable | Không thể xem trước ảnh |
| `user.lightbox.retry` | Retry | Thử lại |
| `user.lightbox.stale` | Showing saved details | Đang hiển thị thông tin đã lưu |
| `user.lightbox.sizes` | Sizes | Kích thước |
| `user.lightbox.size.provisional` | Provisional | Tạm tính |
| `user.lightbox.size.confirmed` | Confirmed | Đã xác nhận |
| `user.lightbox.size.dimensions` | {width} × {height} px | {width} × {height} px |
| `user.lightbox.size.pending` | Confirmed sizes are waiting to be fetched. | Đang chờ tải kích thước đã xác nhận. |
| `user.lightbox.size.unavailable` | Sizes are not available yet. | Chưa có kích thước ảnh. |
| `user.lightbox.size.fetch-first` | Fetch sizes first | Tải kích thước trước |
| `user.lightbox.size.queued` | Size lookup queued. | Đã thêm yêu cầu tải kích thước vào hàng đợi. |
| `user.lightbox.download-size` | Download {size} | Tải {size} xuống |
| `user.lightbox.exif` | Camera details | Thông tin máy ảnh |
| `user.lightbox.exif.camera` | Camera | Máy ảnh |
| `user.lightbox.exif.lens` | Lens | Ống kính |
| `user.lightbox.exif.exposure` | Exposure | Phơi sáng |
| `user.lightbox.exif.original` | Original dimensions | Kích thước bản gốc |
| `user.lightbox.caption` | AI caption | Chú thích AI |
| `user.lightbox.caption.queued` | Caption is waiting to be generated. | Đang chờ tạo chú thích. |
| `user.lightbox.tags` | Tags | Thẻ |
| `user.lightbox.flickr` | View on Flickr | Xem trên Flickr |
| `user.lightbox.favorited-by` | Favorited by | Được yêu thích bởi |
| `user.lightbox.favorited-more` | and {count} more | và {count} người khác |
| `user.lightbox.removed` | This photo is no longer available on Flickr. | Ảnh này không còn trên Flickr. |
| `user.lightbox.forbidden` | This photo is private or unavailable. | Ảnh này ở chế độ riêng tư hoặc không khả dụng. |

## Test IDs

Stable E2E hooks:

- `data-testid="photo-lightbox"`
- `data-testid="lightbox-stage"`
- `data-testid="lightbox-image"`
- `data-testid="lightbox-position"`
- `data-testid="lightbox-previous"`
- `data-testid="lightbox-next"`
- `data-testid="lightbox-close"`
- `data-testid="lightbox-tray-add"`
- `data-testid="lightbox-download"`
- `data-testid="lightbox-owner"`
- `data-testid="lightbox-caption"`
- `data-testid="lightbox-sizes"`
- `data-testid="lightbox-exif"`
- `data-testid="lightbox-tags"`
- `data-testid="lightbox-flickr-link"`
- `data-testid="lightbox-favorited-by"`
- `data-testid="lightbox-loading"`
- `data-testid="lightbox-empty"`
- `data-testid="lightbox-error"`
- `data-testid="lightbox-stale"`
- `data-testid="lightbox-tombstone"`
- `data-testid="lightbox-forbidden"`
- `data-testid="lightbox-retry"`

## Out of scope

Photo editing or deletion, Flickr writes, comments, slideshow or swipe gesture,
zoom controls, full gallery browsing, list sorting or pagination, new download
flow, admin viewer, direct Flickr API calls from the browser, and backend or
OpenAPI implementation in this design ticket.

## Data

See [Data and API](#data-and-api).

## Interactions

See [Actions](#actions).

## Test hooks

See [Test IDs](#test-ids); every hook uses the `data-testid` attribute.
