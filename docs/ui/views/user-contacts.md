# Contacts

View id `user-contacts`, role USER, light gallery theme (dark optional).
Route `/contacts`. Baseline: `vContacts()` in the approved
[POC](../poc/xflickr-poc.html), USER role. Shared rules: [design
system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-contacts.html](../mockups/user-contacts.html). Build ticket:
JOOXF-173.

## Purpose

Show the signed-in user's discovered Flickr contacts, recent photo activity,
interest depth, and crawl progress. Make it quick to find, star, and open a
contact. Depth 1 contacts come from connected Flickr accounts; deeper contacts
are owners discovered from favorites, bounded by this user's effective depth
([business rules](../../SPEC.md#crawl-depth)).

## Route and access

- Route `/contacts`, USER role, active user session required. The server derives
  the user from the session; no user id is sent by the browser.
- An unauthenticated request follows the USER shell's expired-session flow and
  returns to `/contacts` after sign-in. A forbidden response uses the standard
  access-denied view. Document title is `Contacts · XFlickr`; route entry moves
  focus to the page `h1`.
- A contact card opens `/contacts/{nsid}`. Contact detail and tabs belong to the
  Contact view.

## Layout

Inside the USER shell main column (max width 1200px; 24px desktop and 16px
mobile padding), in this order:

1. Page heading `Contacts`, one-line explanation of depth 1 and discovered
   contacts, and the user's effective crawl depth.
2. Search field and filter chips: All, Starred, Crawling, Needs attention.
   Show each filter's result count where available. Search covers display name,
   username/handle, and NSID; submit on Enter or after a 250ms debounce.
3. Sort control: Recently updated (default), Name, Most photos. Keep selected
   filter, query, sort, and cursor in the URL query string so refresh/back
   restores the same result set.
4. A responsive grid of contact cards. Each card has a three-image mosaic (one
   large image and two small), overlapping circular avatar, name, `@username`,
   depth, star toggle, photos/favorites/size counts, crawl status pill, and a
   labelled progress bar. Cards use the USER gallery surface, 14px radius, and
   existing mosaic, avatar, status pill, chip, and progress components.
5. A Load more control when the next cursor exists. Append results without
   moving focus. No separate pagination footer.

## Data and API

The existing API does not provide a contacts list. Do not call Flickr from the
browser. The list operation below is a contract request to be delivered before
the JOOXF-173 build. `GET /me/crawl/interests` is already requested by the Crawl
view; extend its response for this view rather than adding another list
operation. Existing user-scoped crawl settings and star operations are used.

| Element | Source | Fields used |
| --- | --- | --- |
| Contact cards and filter counts | `GET /me/crawl/interests?filter=&q=&sort=&cursor=&limit=30` **contract request / extension** | `items[]`: `person_id`, `nsid`, `name`, `username`, `avatar_url`, `depth`, `starred`, `status` (`done`, `crawling`, `checking`, `queued`, `paused`, `error`), `photos_crawled`, `photos_total`, `favorites_count`, `sizes_percent`, `last_check_at`, `recent_photos[]` (`id`, `thumbnail_url`, `alt`); `counts` by `all`, `starred`, `crawling`, `needs_attention`; `next_cursor`, `updated_at` |
| Effective depth | `GET /me/crawl/settings` (exists; shared with Crawl view) | `depth` |
| Star toggle | `PUT /me/contacts/{nsid}/star` (exists), body `{starred: boolean}` | 204 success; 400/401/404 error |
| Crawl state refresh | `GET /events` (exists, authenticated SSE; use shell connection) | `crawl.progress` `{person_id, status, photos_crawled, sizes_percent}`; `crawl.queue` snapshot where emitted by the user channel |

The list request accepts `filter` (`all`, `starred`, `crawling`,
`needs_attention`), `q` (trimmed search text), `sort` (`updated`, `name`,
`photos`), `cursor`, and `limit` (server caps at 100). Response ordering is
stable within a snapshot; `updated` sorts by `last_check_at` descending with
NSID ascending as a tie-break, `name` sorts case-insensitively with NSID
tie-break, and `photos` sorts by `photos_total` descending with NSID tie-break.
Counts describe the full user-scoped matching result set before the page limit.
Search and filtering apply before pagination. `recent_photos` contains up to
three eligible public or user-visible stored thumbnails, newest first; return
fewer when unavailable. Never return another user's private observations,
Flickr credentials, or a browser-constructed Flickr URL. Missing images use the
shared neutral image placeholder. Missing names use NSID; missing avatar uses
initials fallback.

The `crawling` filter includes `crawling`, `checking`, and `queued`; the
`needs_attention` filter includes `paused` and `error`, matching the POC's
status grouping. Render `checking` with the localized Crawling label. The card
progress bar is photo crawl progress (`photos_crawled / photos_total`, clamped
to 0–100); size readiness is the separate `sizes_percent` count. A zero photo
total renders 0% and does not imply that crawling is complete. The selected
sort's ordering and tie-breaks are defined above; `updated` uses the crawl
record's `last_check_at`.

Contract request additions: list fields `username`, `favorites_count`,
`recent_photos`, `counts`, `updated_at`, the `checking` status value, and
`q`/`sort` query parameters, plus `needs_attention` filter naming. These are
required for JOOXF-173 and must be added to OpenAPI by that build ticket. The
existing star route is the only write action in this view.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Search | Type or submit search field | Debounce 250ms, request matching list from the first page, and update `q`; clear restores all names. |
| Filter | Filter chip | Set `filter`, clear cursor, fetch the first page, and preserve query and sort. |
| Sort | Sort select | Set `sort`, clear cursor, and fetch the first page. |
| Open contact | Card or its name/avatar | Navigate to `/contacts/{nsid}`; keyboard activation is supported. |
| Star / unstar | Star button | Optimistically update `starred`, call `PUT /me/contacts/{nsid}/star`; rollback on failure and show a localized error. The star control does not open the card. |
| Load more | Button after the grid | Request `next_cursor`, append without losing current cards or focus; disable while loading. |
| Retry | Error state button | Retry the failed list request using current query, filter, and sort. |
| Empty-state CTA | Button in empty state | Go to `/onboarding` when no contacts exist; otherwise clear search and set filter to All. |

## States

### Ready

This is the normal state. Render heading, controls, cards, and Load more as
appropriate. A successful empty result uses the Empty state below. Star state
and counts come from the latest response; never infer completion from the
progress percentage alone.

### Loading

For the first request show heading/control placeholders and six card skeletons
with final card geometry; hide skeletons from assistive technology. For search,
filter, or sort refreshes keep current cards visible and show a small loading
indicator near the controls. For Load more retain cards and show an inline card
skeleton. Disable the active submit/load control. Reduced motion uses static
skeletons.

### Empty

If the first unfiltered request succeeds with no contacts, show “No contacts
yet”, explain that contacts appear after the first crawl pass, and offer “Set up
Flickr” to `/onboarding`. If search/filter returns zero while contacts exist,
show “No contacts match” and “Change the search or filter to see contacts.”
with “Clear filters”. Hide Load more. A zero result is not an error.

### Error

If the initial list request fails, show “Could not load contacts”, “Check your
connection, then try again.”, and “Retry”; do not show an empty state. If a
later page fails, retain loaded cards and show an inline retry. A star request
failure restores the previous star value and shows “Could not update star”.
Never display raw server error text. A 401 follows session expiry handling.

### Partial / stale

On refresh failure with usable data, keep cards and show “Contacts may be out of date” with “Retry”. If SSE disconnects, retain the latest successful data, show the shell offline/stale indicator, and poll as described under Live updates. Show `updated_at` as “Updated {relative_time}” when data is more than one minute old. A per-image load failure uses the neutral image placeholder without making the whole list partial.

### Forbidden

For 403 show the standard access-denied state and a “Back to home” link. Clear
any cached list data for this route; do not render stale cards.

## Live updates

This view is live. Use the shell's authenticated `GET /events` SSE connection;
do not open another stream. On `crawl.progress`, update the matching card by
`person_id` for status, `photos_crawled`, and `sizes_percent`; do not reorder
cards solely because progress changed. Coalesce a `crawl.queue` event or a
star mutation for 500ms, then refresh counts and the visible page while keeping
focus. Ignore events for contacts outside the current result. Resume SSE using
`Last-Event-ID` as specified by the shell.

If SSE is unavailable or disconnected for 5 seconds, poll the current list URL
every 30 seconds while the document is visible. Pause polling while hidden and
refresh once on visibility return. Do not poll faster than 30 seconds. Stop the
timer on route exit. A refresh failure keeps usable data and enters
Partial / stale.

## Responsive

- At 1100px and above use three card columns; 700–1099px use two; below 700px
  use one. At 390px, card mosaic remains 16:9, controls wrap, and card actions
  have at least 44px targets.
- Search uses available width; filter chips may scroll horizontally as a group
  without shrinking labels. Sort control moves to a new row when needed.
- At 1440px use the 1200px content maximum and 24px side padding. At 390px use
  16px padding. At 200% zoom and with Vietnamese copy, content wraps without
  horizontal page scrolling or clipped controls.

## Accessibility

- One `h1`; search has a visible label; filters use a labelled group of buttons
  with `aria-pressed`; sort is a labelled native select. Cards form a labelled
  list. Card navigation uses an anchor; star is a separate button with
  `aria-pressed` and a localized name (`Star {name}` / `Unstar {name}`).
- Progress exposes `role="progressbar"`, localized label, min/max/value, and a
  text percentage. Status always includes text and is not conveyed by color
  alone. Mosaic thumbnails are decorative (`alt=""`); avatar fallback does
  not replace the contact link's accessible name.
- Focus follows the 2px accent outline. Enter activates links/buttons; star
  activation does not trigger card navigation. Announce result count and
  explicit Load more completion politely; do not announce every live update.
  Error messages use `role="alert"`; honor reduced motion.

## i18n

English (`en`) and Vietnamese (`vi`) values cover every view-owned label, state
message, and accessible control name. Contact names and usernames are data and
are not translated. Shared shell strings remain owned by `user-shell`.

| Key | EN | VI |
| --- | --- | --- |
| `user.contacts.title` | Contacts | Liên hệ |
| `user.contacts.description` | Your contacts and the people discovered through their favorites. Crawl depth: {depth}. | Liên hệ của bạn và những người được khám phá qua mục yêu thích của họ. Độ sâu thu thập: {depth}. |
| `user.contacts.search.label` | Search contacts | Tìm liên hệ |
| `user.contacts.search.placeholder` | Name, username, or NSID | Tên, tên người dùng hoặc NSID |
| `user.contacts.filter.label` | Filter contacts | Lọc liên hệ |
| `user.contacts.filter.all` | All | Tất cả |
| `user.contacts.filter.starred` | Starred | Đã đánh dấu sao |
| `user.contacts.filter.crawling` | Crawling | Đang thu thập |
| `user.contacts.filter.needs_attention` | Needs attention | Cần chú ý |
| `user.contacts.sort.label` | Sort by | Sắp xếp theo |
| `user.contacts.sort.updated` | Recently updated | Cập nhật gần đây |
| `user.contacts.sort.name` | Name | Tên |
| `user.contacts.sort.photos` | Most photos | Nhiều ảnh nhất |
| `user.contacts.card.open` | View {name} | Xem {name} |
| `user.contacts.card.depth` | Depth {depth} | Cấp độ {depth} |
| `user.contacts.card.photos` | {count} photos | {count} ảnh |
| `user.contacts.card.favorites` | {count} favorites | {count} mục yêu thích |
| `user.contacts.card.sizes` | Sizes {percent}% | Kích thước {percent}% |
| `user.contacts.card.progress` | Photo crawl progress: {percent}% | Tiến độ thu thập ảnh: {percent}% |
| `user.contacts.star.add` | Star {name} | Đánh dấu sao {name} |
| `user.contacts.star.remove` | Unstar {name} | Bỏ đánh dấu sao {name} |
| `user.contacts.status.done` | Complete | Hoàn tất |
| `user.contacts.status.crawling` | Crawling | Đang thu thập |
| `user.contacts.status.checking` | Crawling | Đang thu thập |
| `user.contacts.status.queued` | Queued | Đang chờ |
| `user.contacts.status.paused` | Paused | Đã tạm dừng |
| `user.contacts.status.error` | Needs attention | Cần chú ý |
| `user.contacts.loading.label` | Loading contacts | Đang tải liên hệ |
| `user.contacts.empty.title` | No contacts yet | Chưa có liên hệ |
| `user.contacts.empty.text` | Contacts of your Flickr accounts appear after the first crawl pass. | Liên hệ từ tài khoản Flickr sẽ xuất hiện sau lượt thu thập đầu tiên. |
| `user.contacts.empty.setup` | Set up Flickr | Thiết lập Flickr |
| `user.contacts.empty.filtered.title` | No contacts match | Không có liên hệ phù hợp |
| `user.contacts.empty.filtered.text` | Change the search or filter to see contacts. | Thay đổi nội dung tìm kiếm hoặc bộ lọc để xem liên hệ. |
| `user.contacts.empty.clear` | Clear filters | Xóa bộ lọc |
| `user.contacts.error.title` | Could not load contacts | Không thể tải liên hệ |
| `user.contacts.error.text` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.contacts.error.retry` | Retry | Thử lại |
| `user.contacts.error.star` | Could not update star | Không thể cập nhật dấu sao |
| `user.contacts.partial.title` | Contacts may be out of date | Danh sách liên hệ có thể chưa được cập nhật |
| `user.contacts.partial.retry` | Retry | Thử lại |
| `user.contacts.stale.updated` | Updated {relative_time} | Đã cập nhật {relative_time} |
| `user.contacts.load-more` | Load more | Tải thêm |
| `user.contacts.loading.more` | Loading more contacts | Đang tải thêm liên hệ |
| `user.contacts.forbidden.title` | You cannot view these contacts | Bạn không thể xem các liên hệ này |
| `user.contacts.forbidden.back` | Back to home | Về trang chủ |

## Test IDs

E2E hooks are stable `data-testid` attributes attached to the named controls/elements:

- `user-contacts-page`, `user-contacts-title`, `user-contacts-search`,
  `user-contacts-filter-{all|starred|crawling|needs_attention}`,
  `user-contacts-sort`, `user-contacts-list`, `user-contact-card`,
  `user-contact-star`, `user-contact-status`, `user-contact-progress`,
  `user-contacts-empty`, `user-contacts-error`, `user-contacts-retry`,
  `user-contacts-load-more`, `user-contacts-loading`,
  `user-contacts-stale`.

## Out of scope

Contact detail and tabs, changing crawl depth, selecting or pausing contacts,
bulk actions, contact deletion, direct Flickr requests, and changes to the
OpenAPI files. The necessary list contract request is documented above for its
owning build ticket.

## Layout

Compatibility heading required by the catalogue gate; the full layout is
specified above.

## Data

See [Data and API](#data-and-api).

## Interactions

See [Actions](#actions).

## Test hooks

See [Test IDs](#test-ids).
