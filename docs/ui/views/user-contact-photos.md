# Contact: Header and Photos tab

View id `user-contact-photos`, role USER, light gallery theme (dark optional).
Route `/contacts/:nsid`, default tab `photos`. Baseline: `vContact()` and the
USER role in the approved [POC](../poc/xflickr-poc.html), with tokens and
components from the [design system](../design-system.md) and
[components](../components.md). Shared rules: [patterns](../patterns.md),
[accessibility](../a11y.md), and [i18n](../i18n.md). Mockup:
[user-contact-photos.html](../mockups/user-contact-photos.html). Build ticket:
JOOXF-173. This view renders inside the USER shell.

## Purpose

Show a contact's identity, discovery path, crawl progress, and stored photos.
Let the signed-in user manage this contact's crawl priority and open a download
of all its photos.

## Route and access

- Require an active USER session. Unauthenticated users follow the shell's
  expired-session route. The server derives the XFlickr user from the session;
  never send a user id from the browser.
- `/contacts/:nsid` and `/contacts/:nsid?tab=photos` select Photos. Contact tabs
  preserve the URL-encoded Flickr NSID and update the query without a full page
  reload. Unknown contacts show not found; inaccessible contacts show
  forbidden. Never substitute another contact or retain data after 403/404.
- Document title is `{contact_name} · XFlickr`. On route navigation, focus the
  page `h1`.

## Layout

Inside the USER shell main column (max width 1200px, 24px desktop padding,
16px mobile padding), in this order:

1. Contacts breadcrumb.
2. Cover header with the contact's photo cover, circular avatar, display name,
   Flickr handle, location, depth, and ordered “Found via” discovery chain.
   Missing cover uses neutral decorative art; missing avatar uses deterministic
   initials. Missing location or discovery source uses localized “Not
   available.”
3. Header actions: Star, Prioritise sizes, Pause/Resume contact crawl, and
   primary Download all. Show the confirmed star and pause states. Download all
   opens the shared New download modal preselected to this contact.
4. Crawl block with a text-labelled status pill, photo records crawled/total,
   stored photo sizes percentage, and a progress row for each facet: Photos,
   Favorites, Albums, Galleries, and Groups. A facet may show completed/total,
   percentage, and its status. Do not infer totals or completion when absent.
5. Contact tab bar with Photos selected and counts for Photos, Favorites,
   Albums, Galleries and Groups. About has no count. Other tabs are links to
   their separately specified views.
6. Photos tab uses the existing justified `photo-grid` / `photo-card` from
   Library. Use the same crop, hover, selection-tray, lightbox and pagination
   behavior as Library. This view adds no photo action. Keep the header and
   crawl block above the grid in every Photos state.

## Data and API

Do not call Flickr from the browser. The backend applies Flickr visibility and
user-sharing rules; a response must not expose records visible only through
another user's private access. Existing operations are marked “existing”; all
other operations and fields are contract requests for JOOXF-173 and must be
added to OpenAPI before the build. Use the authenticated session for scope.

| Element | Source | Fields used |
| --- | --- | --- |
| Contact identity, cover, discovery, crawl, counts and facet progress | `GET /me/contacts/{nsid}` **contract request** | `nsid`, `username`, `real_name`, `avatar_url`, `cover_photo_url`, `location`, `depth`, `found_via[]` (`nsid`, `label`), `starred`, `paused`, `photo_records_crawled`, `photo_records_total`, `sizes_completed`, `facets[]` (`name`, `completed`, `total`, `status`, `updated_at`), `counts` (`photos`, `favorites`, `albums`, `galleries`, `groups`), `updated_at` |
| Photos grid | `GET /me/contacts/{nsid}/photos?cursor=&limit=30` **contract request** | `items[]`: `id`, `title`, `thumbnail_url`, `width`, `height`, `taken_at`, `sizes_available`; `total`, `next_cursor`, `updated_at` |
| Star | `PUT /me/contacts/{nsid}/star` **existing** | request `starred` |
| Select contact for seven days | `PUT /me/contacts/{nsid}/select` **existing** | path `nsid`; this changes scheduler selection but does not guarantee sizes-first order |
| Prioritise sizes | `PUT /me/contacts/{nsid}/sizes-priority` **contract request** | no body; prioritize this contact's pending size work using the user's reserved-call policy |
| Pause / resume contact | `PUT /me/crawl/pause` and `PUT /me/crawl/resume` **existing** | request `nsid` |
| New download | `POST /downloads` **existing** | request `kind: "contact"`, `subject: nsid`, plus `photo_ids` only for tray downloads |
| Live refresh | `GET /events` **existing** | authenticated SSE with resumable event IDs; `crawl.progress` is user scoped. Contact refresh event `contact.updated` with `nsid`, `updated_at` is a **contract request** |

`sizes_completed` is a percentage from 0 to 100. Facet status is `pending`,
`running`, `complete`, `partial` or `failed`; `total` can be null. Counts are
nonnegative. `found_via` is the ordered path from a root connected account;
depth is this user's minimum interest depth, not a global crawl depth. Missing
photo images use the shared placeholder and never a broken-image glyph. Photo
URLs are stored size URLs selected by the server. Do not persist these
responses in browser storage.

The contract request for `sizes-priority` is needed because the existing
seven-day selection endpoint raises general contact priority but does not
promise size work runs first. The POC's “Fetch sizes first” control and this
ticket's “Prioritise sizes” action require that narrower behavior. No other
new endpoint is required for this view.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Browse contacts | Breadcrumb | Navigate to `/contacts`. |
| Star / unstar | Star button | Send `PUT /me/contacts/{nsid}/star`; update only after confirmation, with a pending disabled state. |
| Prioritise sizes | Header button | Send contract-requested `PUT /me/contacts/{nsid}/sizes-priority`; show confirmation and refresh relevant progress. |
| Pause crawl | Crawl button when running or queued | Send `PUT /me/crawl/pause` with `nsid`; retain work and stop dispatch for this user/contact. |
| Resume crawl | Crawl button when paused | Send `PUT /me/crawl/resume` with `nsid`; work returns to normal scheduling. |
| Download all | Header button | Open New download modal with contact scope and source preselected. Submission uses existing `POST /downloads` with `kind: "contact"` and `subject: nsid`; back-pressure errors are shown in the modal. |
| Open photo | Photo tile, excluding any Library tray control | Open shared lightbox; closing returns focus to the tile. |
| Change contact tab | Tab link | Navigate to that contact section and preserve `nsid`. |
| Load more | Grid continuation control | Fetch `next_cursor`, append photos without moving focus; hide when no cursor remains. |
| Retry | Error banner button | Retry only the failed request. |

## States

### Ready

Show the complete header, confirmed action state, crawl summary and five facet
progress rows, count-bearing tab bar, and Photos grid. Announce the initial
photo total once. A zero photos count uses Empty. Missing optional values use
the specified localized fallback. Photo ordering and counts come from the API.

### Loading

For initial load, show cover/header skeletons, crawl/facet skeletons, tab labels
with known counts when available, and six photo-card skeletons in justified
geometry. Keep known header data visible during a grid-only fetch. For Load more,
keep photos visible and append an inline skeleton; disable that control while
the request is active. Hide skeletons from assistive technology and make them
static under reduced motion.

### Empty

After a successful first photo response with `total: 0`, show “No photos yet”
and “Photos from this contact will appear here after XFlickr crawls them.” The
CTA “Browse contacts” returns to `/contacts`. Header, crawl block and tabs remain
visible. Empty is a successful zero result, never an error.

### Error

If initial contact or photos loading fails, show “Could not load contact photos”
and “Check your connection, then try again.” with “Retry”. Preserve usable
header data but do not show Empty. A later-page failure retains existing photos
and offers inline Retry for that page. Never display server error text. A 401
uses shell session expiry handling.

### Partial / stale

When one resource fails, render successful resources and show a warning in the
failed resource's location: “Some contact information may be out of date” or
“Some photos may be out of date”, with Retry. Keep last successful data during
refresh. If SSE drops, show the shell offline/stale indicator and poll as
specified below. Show “Updated {relative_time}” when data is older than one
minute. Do not flash skeletons over usable content.

### Forbidden

For 403, show “You cannot view this contact”; for 404, show “Contact not
found”. Both provide “Back to contacts”. Remove contact and photo data from the
view and do not retry automatically. A 401 follows session expiry behavior.

## Live updates

This view is live while visible. Subscribe to authenticated `GET /events`,
resume with `Last-Event-ID`, and filter contract-requested `contact.updated` by
exact `nsid`; apply user-scoped `crawl.progress` only to this user's matching
contact. Coalesce matching events for 500ms, then refresh contact progress and
the first photo page. Discard later pages after a photo refresh so the grid
cannot mix snapshots. Keep focus and announce only a concise updated status,
never every inserted photo.

If SSE is unavailable or disconnected for 5 seconds, poll the contact and first
photo page every 30 seconds while the tab is visible. Pause polling while
hidden; refresh once on visibility. Reconnect SSE with the shell's backoff and
stop polling/timers on navigation. Never poll faster than 30 seconds.

## Responsive

- At 900px and above, use four justified columns in the 1200px content column;
  600–899px uses three; below 600px uses two. At 390px preserve two columns,
  at least 8px gutters, and source aspect ratios.
- At 1440px the cover spans the content column and its actions sit alongside
  identity. At 390px the cover, identity and actions wrap; actions remain
  named, visible and comfortably tappable. Crawl rows wrap without hiding
  facets. Tabs may scroll horizontally but labels do not shrink.
- Allow English and Vietnamese expansion of at least 30%. At 200% zoom and
  390px there is no horizontal page scrolling. Photo cards use `object-fit:
  cover`; do not clip identity, status, progress labels or actions.

## Accessibility

- Use one `h1`, breadcrumb navigation, and contact tab navigation labelled
  “Contact sections”. Photos exposes `aria-current="page"`.
- Name buttons with visible text; Star exposes pressed state, and Pause/Resume
  exposes the action and confirmed crawl state. Status and progress include
  text or accessible values; color alone never communicates meaning.
- Use a labelled list/grid for photos. Photo image alt text uses its title;
  decorative cover art has empty alt. Photo tile and tray control are separate
  keyboard targets. Escape closes the lightbox and restores focus.
- Announce initial result count, retry errors, and action completion politely.
  Errors use `role="alert"`; skeletons are hidden from assistive technology.
  Focus uses the design-system outline. Honor reduced motion.

## i18n

English and Vietnamese strings introduced by this view are listed below.
Shared shell, Library photo-grid, lightbox, and New download modal strings use
their owning catalogs. Flickr names, handles, locations, photo titles, and
dates are source data and are not translated.

| Key | EN | VI |
| --- | --- | --- |
| `user.contact-photos.title` | {contact_name} · XFlickr | {contact_name} · XFlickr |
| `user.contact-photos.breadcrumb.contacts` | Contacts | Liên hệ |
| `user.contact-photos.tabs.label` | Contact sections | Các mục liên hệ |
| `user.contact-photos.tabs.photos` | Photos | Ảnh |
| `user.contact-photos.tabs.favorites` | Favorites | Yêu thích |
| `user.contact-photos.tabs.albums` | Albums | Album |
| `user.contact-photos.tabs.galleries` | Galleries | Bộ sưu tập |
| `user.contact-photos.tabs.groups` | Groups | Nhóm |
| `user.contact-photos.tabs.about` | About | Giới thiệu |
| `user.contact-photos.identity.handle` | @{handle} | @{handle} |
| `user.contact-photos.identity.location` | {location} | {location} |
| `user.contact-photos.identity.depth` | Depth {depth} | Cấp độ {depth} |
| `user.contact-photos.identity.found-via` | Found via {source} | Được tìm thấy qua {source} |
| `user.contact-photos.value.unavailable` | Not available | Không có thông tin |
| `user.contact-photos.action.star` | Star | Đánh dấu sao |
| `user.contact-photos.action.starred` | Starred | Đã đánh dấu sao |
| `user.contact-photos.action.unstar` | Unstar | Bỏ đánh dấu sao |
| `user.contact-photos.action.prioritise-sizes` | Prioritise sizes | Ưu tiên tải kích thước ảnh |
| `user.contact-photos.action.prioritise-sizes.success` | Photo sizes prioritised | Đã ưu tiên tải kích thước ảnh |
| `user.contact-photos.action.pause` | Pause crawl | Tạm dừng thu thập |
| `user.contact-photos.action.resume` | Resume crawl | Tiếp tục thu thập |
| `user.contact-photos.action.pause.success` | Contact crawl paused | Đã tạm dừng thu thập liên hệ |
| `user.contact-photos.action.resume.success` | Contact crawl resumed | Đã tiếp tục thu thập liên hệ |
| `user.contact-photos.action.download-all` | Download all | Tải tất cả |
| `user.contact-photos.crawl.title` | Crawl | Thu thập dữ liệu |
| `user.contact-photos.crawl.status.pending` | Pending | Đang chờ |
| `user.contact-photos.crawl.status.running` | Crawling | Đang thu thập |
| `user.contact-photos.crawl.status.complete` | Complete | Hoàn tất |
| `user.contact-photos.crawl.status.partial` | Partial | Một phần |
| `user.contact-photos.crawl.status.failed` | Failed | Thất bại |
| `user.contact-photos.crawl.status.paused` | Paused | Đã tạm dừng |
| `user.contact-photos.crawl.photos` | Photo records {done} / {total} | Bản ghi ảnh {done} / {total} |
| `user.contact-photos.crawl.sizes` | Sizes {percent}% | Kích thước {percent}% |
| `user.contact-photos.crawl.facets` | Facet progress | Tiến độ theo danh mục |
| `user.contact-photos.facet.photos` | Photos | Ảnh |
| `user.contact-photos.facet.favorites` | Favorites | Yêu thích |
| `user.contact-photos.facet.albums` | Albums | Album |
| `user.contact-photos.facet.galleries` | Galleries | Bộ sưu tập |
| `user.contact-photos.facet.groups` | Groups | Nhóm |
| `user.contact-photos.facet.progress` | {done} of {total} · {status} | {done} / {total} · {status} |
| `user.contact-photos.empty.title` | No photos yet | Chưa có ảnh |
| `user.contact-photos.empty.text` | Photos from this contact will appear here after XFlickr crawls them. | Ảnh của liên hệ này sẽ xuất hiện tại đây sau khi XFlickr thu thập dữ liệu. |
| `user.contact-photos.empty.cta` | Browse contacts | Xem danh sách liên hệ |
| `user.contact-photos.error.title` | Could not load contact photos | Không thể tải ảnh liên hệ |
| `user.contact-photos.error.text` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.contact-photos.error.retry` | Retry | Thử lại |
| `user.contact-photos.partial.contact` | Some contact information may be out of date | Một số thông tin liên hệ có thể chưa được cập nhật |
| `user.contact-photos.partial.photos` | Some photos may be out of date | Một số ảnh có thể chưa được cập nhật |
| `user.contact-photos.stale.updated` | Updated {relative_time} | Đã cập nhật {relative_time} |
| `user.contact-photos.forbidden.title` | You cannot view this contact | Bạn không thể xem liên hệ này |
| `user.contact-photos.not-found.title` | Contact not found | Không tìm thấy liên hệ |
| `user.contact-photos.forbidden.back` | Back to contacts | Quay lại danh sách liên hệ |
| `user.contact-photos.loading.label` | Loading contact photos | Đang tải ảnh liên hệ |
| `user.contact-photos.loading.more` | Loading more photos | Đang tải thêm ảnh |
| `user.contact-photos.grid.label` | Photos by {contact_name} | Ảnh của {contact_name} |
| `user.contact-photos.grid.count` | {count, plural, one {# photo} other {# photos}} | {count} ảnh |
| `user.contact-photos.load-more` | Load more | Tải thêm |
| `user.contact-photos.updated` | Updated | Đã cập nhật |

## Test IDs

Stable E2E selectors use `data-testid` and do not vary by locale or theme:

- `data-testid="user-contact-photos-page"`
- `data-testid="user-contact-photos-header"`
- `data-testid="user-contact-photos-avatar"`
- `data-testid="user-contact-photos-star"`
- `data-testid="user-contact-photos-prioritise-sizes"`
- `data-testid="user-contact-photos-crawl-toggle"`
- `data-testid="user-contact-photos-download-all"`
- `data-testid="user-contact-photos-crawl"`
- `data-testid="user-contact-photos-facet-{facet}"`
- `data-testid="user-contact-photos-tabs"`
- `data-testid="user-contact-photos-grid"`
- `data-testid="user-contact-photos-photo-{photo_id}"`
- `data-testid="user-contact-photos-loading"`
- `data-testid="user-contact-photos-empty"`
- `data-testid="user-contact-photos-error"`
- `data-testid="user-contact-photos-partial"`
- `data-testid="user-contact-photos-forbidden"`
- `data-testid="user-contact-photos-retry"`
- `data-testid="user-contact-photos-load-more"`
- `data-testid="user-contact-photos-live-status"`

## Out of scope

Favorites, albums, galleries, groups and About content; contact editing or
deletion; changing crawl depth; API implementation and OpenAPI edits; Library
filters and sorting; downloads UI beyond opening the shared New download modal;
other views. The sizes-priority API addition is a contract request for the
build ticket, not an OpenAPI change in this design ticket.

## Data

Compatibility index for the UI-spec gate: sources and fields are defined in
[Data and API](#data-and-api).

## Interactions

Compatibility index for the UI-spec gate: behavior is defined in
[Actions](#actions).

## Test hooks

Compatibility index for the UI-spec gate: selectors are defined in
[Test IDs](#test-ids).
