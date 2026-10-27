# Library

View id `user-library`, role USER, light gallery theme (dark optional), route
`/library`. Parent: JOOXF-64. Build ticket: JOOXF-170. Baseline: `vLibrary()`,
`libPhotos()` and `jitem()` in the approved [POC](../poc/xflickr-poc.html),
USER role. Follow the [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md) and [i18n](../i18n.md). Mockup:
[user-library.html](../mockups/user-library.html). Render inside the
[user shell](user-shell.md).

## Purpose

Browse the signed-in user's visible photo records, narrow them by source,
readiness and crawl depth, and add photos to a download tray.

## Route and access

- `/library`, USER role; document title `Library · XFlickr`; Library shell item
  active. Missing/expired sessions follow user-shell sign-in handling;
  ADMIN is redirected to `/admin` by the shell.
- Respect the saved theme or USER system preference. Default is light gallery;
  dark gallery is supported. Set `data-role="user"`.
- Query state: repeat `contact` parameters for selected contact NSIDs and
  repeat `depth` parameters for selected depths; `kind`, `sizes_ready`,
  `captioned`, `sort`, and `cursor` are single values. Omit default values.
  Clear removes all filters and cursor. A `photo` query is not supported;
  photo detail/lightbox is outside this view.
- Server scopes every result to the authenticated user and Flickr visibility.
  The client sends no user id.

## Layout

At 1440 px, use the shell main column (max-width 1200 px, standard 24 px
gutter). Page head has `h1` Library, a one-line lead with total visible record
count, and a Download tray button with selected count. Below it, a 220 px
filter sidebar sits beside a min-width-zero results section with 26 px gap.
Sidebar groups: Kind (Everything, Favorites, In albums, Galleries), Contact
(searchable checkbox list), Readiness (Sizes ready, Captioned), and Depth
(All depths and each available numeric depth). Show a count beside every
choice, including zero counts; disabled zero-count values remain clearable if
selected. Results toolbar shows current result count and a Sort select
(Newest taken, Most viewed, Most favorited). Results use the justified photo
grid from the POC: natural photo ratios, rounded corners, hover caption with
photo title and contact name, readiness flags for sizes and caption, and a
per-photo tray toggle. Caption remains available on keyboard focus and touch.
No extra photo-card component is introduced.

At widths below 900 px, filters become a horizontally scrollable filter row
above results; each group is a labeled disclosure/popover with selected
summary/count and Apply/Reset. At 390 px, controls wrap without page-level
horizontal scrolling, tiles retain useful width, filter row scrolls
horizontally, and the tray action remains visible. Grid columns use available
width and justified row height; never crop meaningful photo content to fit.
Use existing `photo-grid`, `filter chip`, `button`, `select`, `checkbox`,
`badge`, `skeleton`, `empty-state`, and `banner` patterns only.

The POC does not show depth, caption status, mobile filter disclosure,
selection mode, count semantics or pagination. Decisions: add a depth filter;
show sizes and caption flags; counts are server-side facet counts for the
current other filters; mobile groups disclose a popover; the toolbar's Select
photos control enters selection mode and exposes checkboxes on cards; and
pagination uses an explicit Load more button with a cursor. These interactions
are keyboard operable and do not introduce another visual component.

## Data and API

No library/list/photos endpoint exists in `contracts/openapi` or the backend.
The list below is a contract request for JOOXF-170's build work; do not call it
before contract and backend implementation. Existing calls are identified
separately.

| Element | Source | Fields used | Action |
| --- | --- | --- | --- |
| Photo pages and totals | `GET /me/library` **contract request**; query repeated `contact` and `depth`, `kind` (`all`, `favorites`, `albums`, `galleries`), `sizes_ready`, `captioned`, `sort` (`taken_desc`, `views_desc`, `favorites_desc`), `cursor`, `limit` (36) | `items[]`: `id`, `title`, `thumbnail_url`, `owner_nsid`, `owner_name`, `taken_at`, `width`, `height`, `sizes_ready`, `captioned`, `depth`, `views_count`, `favorites_count`; response `total`, `next_cursor` | Open a photo is not an action here; toggling tray is local. |
| Filter choices and counts | Same `GET /me/library` contract request | `facets.contacts[]` `{nsid, name, count}`, `facets.kinds[]` `{kind, count}`, `facets.sizes_ready` `{true_count, false_count}`, `facets.captioned` `{true_count, false_count}`, `facets.depths[]` `{depth, count}`; counts reflect all active filters except the facet's own selection | Apply one or more choices; URL reflects filters; count is for matching results. |
| Tray download | `POST /downloads` (exists) body `{kind: "tray", photo_ids: [...]}` | response `id`, `kind`, `subject`, `status`, `created_at`, `expires_at`, `item_count`, `skipped` | Create request; on 201 navigate `/downloads?id={id}`; on 409 map `temp_storage_full` or `flickr_key_required` to localized copy. Existing schema requires 1–500 `photo_ids` entries. |
| Live events | authenticated `GET /events` (exists; shell-owned SSE) | Existing stream documents `crawl.progress`, `download.progress`, `download.ready`; resumes with `Last-Event-ID` | Library refresh behavior below; no new event subscription from the view. |
| Library change event | `GET /events` **contract request** | `library.photo_changed` `{photo_id, changed_fields}` and `library.photo_removed` `{photo_id}` | Update visible card or invalidate the current result/facet snapshot. User scoped; resume using `Last-Event-ID`. |
| Errors | shared `Error` response | `code`, `message` (English, never shown), `details` | Map known codes to localized copy; never display backend message. |

The library response must contain only records visible to this user and
authorized thumbnail URLs. A missing thumbnail uses the neutral placeholder.
Use `taken_at` for taken-date ordering; `views_count` and `favorites_count`
must be added to each item for the two POC sort modes (and remain nonnegative
integers). Filter counts need total/matching semantics specified above. These
fields are part of the `/me/library` contract request, not existing fields.
`owner_nsid` is the stable contact filter value. `depth` is each user's
interest depth for that owner, not a global depth, and populates the depth
facet. Cursor order is stable within a snapshot; a filter/sort change drops
cursor and replaces results.

## Data

The requested `/me/library` snapshot is the source for photo cards, totals,
filter choices and their counts. The existing download and event endpoints are
listed above with the actions each supports.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Filter photos | Kind radio, contact/depth checkbox, readiness checkbox, mobile Apply | Update URL, reset cursor and fetch the first page. Selecting another kind replaces the prior kind; contact and depth support multiple selections. |
| Sort | Sort select | Update `sort`, reset cursor, fetch first page. Default `taken_desc`. |
| Select photos | Select photos button; card checkbox; Select visible / Clear selection | Selection mode reveals card checkboxes. Select visible applies to the loaded page only. Tray selection persists while filters/pages change, and is cleared only by explicit removal, successful tray submission, or leaving the authenticated session. Selection cap is 500, matching `CreateDownloadRequest.photo_ids.maxItems`; at cap, further additions are disabled with copy. |
| Toggle tray item | Card tray toggle | Add/remove `id` from local tray; pressed state and toolbar count update immediately. No API call. |
| Download tray | Page-head button with nonempty tray | `POST /downloads` with `kind: "tray"` and selected `photo_ids`; show pending state; on success clear tray and navigate to the created download. On failure keep selection. |
| Load more | Button after current grid | Request `next_cursor`, append unique items, preserve focus and scroll. Hidden when cursor is null. No automatic/infinite fetch. |
| Clear filters | Clear button | Reset filters and cursor to defaults, retaining tray selection. |
| Retry | Error/retry control | Retry failed list request; retain successful pages when possible. |

## Interactions

- Toolbar-to-grid focus follows the order specified below. Keep selection
  announcements and button pressed states synchronized with the tray.

- Tray control is a separate button from opening a photo card. Card and tray
  controls have distinct accessible names; pressing Space/Enter toggles a card
  only when its explicit control has focus.
- In selection mode, Escape exits selection mode without clearing selected
  photos. The Download tray button is disabled for an empty tray.
- Sort and filter changes keep the results heading in view and focus the first
  result or the empty result message. Load more appends and returns focus to its
  button.
- Debounce contact search by 200 ms; it searches the loaded facet choices and
  does not issue a separate request. Mobile Apply closes the group and updates
  results; Reset clears only that group.
- Combine selected contacts with OR, selected depths with OR, and filter groups
  with AND. Apply the same OR-within/AND-between rule to facet counts; each
  choice count is calculated with its own facet's current selection removed.
- For equal sort values, order by photo `id` descending. The server cursor is
  opaque and bound to the filter, sort, and result snapshot; reject a cursor
  that does not match that request and start a fresh first page.

## States

### Normal

Render the loaded photo grid and current total/facets. If fewer than one page
exists, omit Load more. Status flags use text plus icon, never color alone.

### Ready

Ready is the normal state above. The mockup labels it `ready` to align with the
repository's UI check gate.

### Loading

On first load, after 150 ms show 12 aspect-ratio-preserving photo skeletons,
keep the shell and page title, set `aria-busy="true"` on results and announce
`user.library.status.loading` once. Timeout after 10 seconds becomes Error.
Filter/sort refresh keeps old cards visible with a stale/loading indicator;
Load more appends three row-equivalent skeletons and disables the paging
button. Honor reduced motion.

### Empty

No records and no filters: `user.library.empty.title` and `.text`, CTA to
`/accounts` (connect Flickr) when no connected account exists, otherwise to
`/crawl` (check crawl progress). A filtered result with no matches shows
`user.library.empty.filtered`, identifies active filter labels in the results
heading, and offers `user.library.filters.clear`. Use this same empty treatment
for every kind, contact, readiness, and depth filter and their combinations; do
not substitute an unrelated per-filter message. Empty content is localized and
the CTA is a link.

### Error

Initial list failure (network, timeout, 5xx): replace results with the alert
empty state, `user.library.error.title`, `.text`, and `user.common.retry`.
Retry repeats the request; focus moves to the error block. Page-more failure
keeps existing cards and shows inline `.error.more` with retry. Download
`temp_storage_full` uses `.error.storage`; the existing `flickr_key_required`
code uses `.error.key` with a link to `/keys`. Do not expect `key_required` or
`key_invalid` from this endpoint. Other action failures use
`.error.action`; tray selection remains intact.

### Partial and stale

If a refresh fails after a successful result, retain cards/facets and show
`user.library.status.stale` with Retry. If a page fails, retain prior pages
and show inline retry. When SSE is disconnected or two 30-second fallback
polls fail, show stale status and keep the last snapshot; successful poll or
reconnect clears it. A `photo_removed` event removes that card and adjusts
visible count; if it leaves the active page short, do not silently fetch a new
page.

### Forbidden

401 follows shell expiration handling. A 403 on initial load replaces the
view body with lock icon, `user.library.forbidden.title`, `.text` and link
`.forbidden.cta` to `/`; no retry. A `csrf_rejected` on download refreshes the
CSRF token once and retries once; a second failure uses `.error.action`.

## Live updates

Use the authenticated shell-owned `GET /events` EventSource and resume using
`Last-Event-ID`. Existing crawl progress events do not fabricate a photo item;
they mark library data stale and prompt an immediate refresh if the view is
visible. The requested `library.photo_changed` and `library.photo_removed`
events update matching cards and mark facets stale; coalesce refreshes for
500 ms. If the event stream is unavailable, poll the current list/facets every
30 seconds while the document is visible; pause polling in a hidden tab. After
two failed polls show Partial and stale. On reconnect perform one refresh,
deduplicate by photo `id`, then resume event processing. No optimistic photo
metadata edits are offered.

## Responsive

At >=900 px use 220 px sidebar and flexible grid. At <900 px replace sidebar
with horizontally scrollable labeled filter groups. At 390 px controls and
Vietnamese text may wrap, grid columns preserve a minimum 145 px card width
where possible, and the tray action remains available. No body horizontal
scroll; only the filter row may scroll horizontally. Touch targets are at
least 44 by 44 px; hover captions remain available on focus/tap.

## Accessibility

- Use one `h1`, labeled filter groups (`fieldset`/`legend`), native inputs and
  a labeled sort select. Announce total/result changes with `aria-live="polite"`.
- Every meaningful photo has alt text from title and owner name; neutral
  placeholders are decorative. Card caption is visible on hover and keyboard
  focus. Tray toggle uses `aria-pressed` and an action-specific name.
- Selection mode is announced, focus order follows toolbar, filters, grid,
  paging; errors use `role="alert"`; loading uses `aria-busy` and a status
  message. Focus indicators meet design-system contrast and thickness.
- Pair readiness status colors with text/icons. Respect reduced motion and
  preserve focus after paging and tray updates.

## i18n

All visible copy uses the keys below; EN and VI strings are the approved
initial values. Error API `message` is never displayed. Format counts with
locale-aware number formatting. Do not truncate necessary names/actions.

| Key | EN | VI |
| --- | --- | --- |
| `user.library.title` | Library | Thư viện |
| `user.library.lead` | {count} photos in your library | {count} ảnh trong thư viện |
| `user.library.action.download` | Download tray ({count}) | Tải ảnh đã chọn ({count}) |
| `user.library.action.select` | Select photos | Chọn ảnh |
| `user.library.action.select-visible` | Select visible | Chọn ảnh đang hiển thị |
| `user.library.action.clear-selection` | Clear selection | Bỏ chọn |
| `user.library.selection.status` | Selection mode. {count} photos selected. | Chế độ chọn. Đã chọn {count} ảnh. |
| `user.library.action.load-more` | Load more | Tải thêm |
| `user.library.filters.title` | Filters | Bộ lọc |
| `user.library.filters.kind` | Kind | Loại ảnh |
| `user.library.filters.kind.all` | Everything | Tất cả |
| `user.library.filters.kind.favorites` | Favorites | Ảnh yêu thích |
| `user.library.filters.kind.albums` | In albums | Trong album |
| `user.library.filters.kind.galleries` | Galleries | Bộ sưu tập |
| `user.library.filters.contact` | Contact | Liên hệ |
| `user.library.filters.contact-search` | Search contacts | Tìm liên hệ |
| `user.library.filters.contact-none` | No contacts match this search. | Không có liên hệ phù hợp với tìm kiếm này. |
| `user.library.filters.readiness` | Readiness | Trạng thái |
| `user.library.filters.sizes` | Sizes ready | Đã có kích thước |
| `user.library.filters.captioned` | Captioned | Đã có chú thích |
| `user.library.filters.depth` | Depth | Độ sâu |
| `user.library.filters.all-depths` | All depths | Mọi độ sâu |
| `user.library.filters.depth-option` | Depth {depth} | Độ sâu {depth} |
| `user.library.filters.apply` | Apply | Áp dụng |
| `user.library.filters.reset` | Reset | Đặt lại |
| `user.library.filters.clear` | Clear filters | Xóa bộ lọc |
| `user.library.filters.count` | {count} photos | {count} ảnh |
| `user.library.sort.label` | Sort | Sắp xếp |
| `user.library.sort.taken` | Newest taken | Mới chụp nhất |
| `user.library.sort.views` | Most viewed | Xem nhiều nhất |
| `user.library.sort.favorites` | Most favorited | Yêu thích nhiều nhất |
| `user.library.photo.by` | by {owner} | của {owner} |
| `user.library.photo.sizes-ready` | Sizes ready | Đã có kích thước |
| `user.library.photo.sizes-pending` | Sizes pending | Đang chờ kích thước |
| `user.library.photo.captioned` | Caption ready | Đã có chú thích |
| `user.library.photo.caption-pending` | Caption pending | Đang chờ chú thích |
| `user.library.tray.add` | Add {title} to download tray | Thêm {title} vào danh sách tải |
| `user.library.tray.remove` | Remove {title} from download tray | Xóa {title} khỏi danh sách tải |
| `user.library.tray.limit` | The tray holds up to 500 photos. | Danh sách tải chứa tối đa 500 ảnh. |
| `user.library.status.loading` | Loading your library… | Đang tải thư viện… |
| `user.library.status.stale` | Showing saved results. Could not refresh. | Đang hiển thị kết quả đã lưu. Không thể làm mới. |
| `user.library.empty.title` | Your library is empty | Thư viện của bạn đang trống |
| `user.library.empty.text` | Photos appear here as soon as the crawler stores them. | Ảnh sẽ xuất hiện tại đây khi hệ thống lưu ảnh. |
| `user.library.empty.cta-account` | Connect a Flickr account | Kết nối tài khoản Flickr |
| `user.library.empty.cta-crawl` | Check crawl progress | Kiểm tra tiến độ thu thập |
| `user.library.empty.filtered` | No photos match these filters. | Không có ảnh phù hợp với bộ lọc này. |
| `user.library.error.title` | We couldn’t load your library. | Không thể tải thư viện. |
| `user.library.error.text` | Check your connection and try again. | Hãy kiểm tra kết nối rồi thử lại. |
| `user.library.error.more` | More photos could not be loaded. | Không thể tải thêm ảnh. |
| `user.library.error.storage` | Temporary download storage is full. Try again later. | Bộ nhớ tải tạm đã đầy. Hãy thử lại sau. |
| `user.library.error.key` | Add or check a valid Flickr API key to download. | Thêm hoặc kiểm tra khóa API Flickr hợp lệ để tải ảnh. |
| `user.library.error.action` | That action failed. Your selection is still here. | Thao tác không thành công. Ảnh bạn chọn vẫn còn đây. |
| `user.library.forbidden.title` | You can’t view this library. | Bạn không thể xem thư viện này. |
| `user.library.forbidden.text` | Sign in with an account that can access this library. | Đăng nhập bằng tài khoản có quyền truy cập thư viện này. |
| `user.library.forbidden.cta` | Go to Home | Về trang chủ |
| `user.common.retry` | Try again | Thử lại |

## Test IDs

Use stable selectors below; `{id}` is the photo id.

| `data-testid` | Element |
| --- | --- |
| `user-library-page` | view root |
| `user-library-heading` | page heading |
| `user-library-total` | visible total |
| `user-library-download-tray` | tray download button |
| `user-library-select-mode` | selection mode toggle |
| `user-library-select-visible` | select loaded page |
| `user-library-clear-selection` | clear tray |
| `user-library-filters` | desktop sidebar/mobile filter row |
| `user-library-filter-kind` | kind group |
| `user-library-filter-contact` | contact group |
| `user-library-contact-search` | contact facet search field |
| `user-library-filter-sizes` | sizes-ready checkbox |
| `user-library-filter-captioned` | captioned checkbox |
| `user-library-filter-depth` | depth group |
| `user-library-filter-apply` | mobile filter group Apply button |
| `user-library-filter-reset` | mobile filter group Reset button |
| `user-library-filter-clear` | clear all filters button |
| `user-library-sort` | sort select |
| `user-library-grid` | photo grid |
| `user-library-photo-{id}` | photo card |
| `user-library-photo-caption-{id}` | photo caption |
| `user-library-photo-sizes-{id}` | sizes flag |
| `user-library-photo-captioned-{id}` | caption flag |
| `user-library-photo-tray-{id}` | card tray toggle |
| `user-library-photo-select-{id}` | selection-mode checkbox |
| `user-library-selection-status` | selection mode and selected count announcement |
| `user-library-load-more` | paging button |
| `user-library-load-more-retry` | retry button for paging failure |
| `user-library-loading` | loading state |
| `user-library-empty` | empty state |
| `user-library-error` | error state |
| `user-library-stale` | partial/stale status and retry |
| `user-library-forbidden` | forbidden state |
| `user-library-retry` | retry button |

## Test hooks

E2E tests use the [Test IDs](#test-ids) table and assert route, filter query,
facet counts, paging cursor, tray persistence and the four mockup state shapes.

## Out of scope

- Search view and search API, photo detail/lightbox, contacts/albums views,
  editing captions, starring contacts, permanent archive or cloud sync.
- API contract and backend implementation: the `GET /me/library` and
  `library.photo_changed` / `library.photo_removed` contract requests above
  belong to build/API work. This design ticket changes documentation only.
