# Albums list

View id `user-albums`, role USER, light gallery theme (dark optional). Route
`/albums`. Visual baseline: `vAlbums()` and `albumCard()` in the approved
[POC](../poc/xflickr-poc.html), USER role. Shared rules: [design
system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-albums.html](../mockups/user-albums.html). Build ticket:
JOOXF-174.

## Purpose

Show albums discovered across the signed-in user's contacts and how many
photos XFlickr has stored from each album compared with Flickr's total. This is
a read-only index; opening a card navigates to the album detail view.

## Route and access

- `/albums`, USER role, active user session required. The server scopes all
  results to the session user; the browser sends no user id.
- Unauthenticated users follow the shell's session-expiry flow and return to
  `/albums` after sign-in. A 403 uses the standard access-denied state.
- Document title is `Albums · XFlickr`. On route entry, move focus to the page
  `h1`.

## Layout

Inside the USER shell main column (max width 1200px, 24px desktop padding,
16px mobile padding), render:

1. Page heading `Albums` and brief description, “Albums of all your contacts.”
2. A labelled contact filter with `All contacts` selected by default, followed
   by one option per contact with albums. The selected filter is stored as
   `contact` in the query string; selecting All removes it. Unknown or
   inaccessible contact ids reset to All and show the standard inline notice.
3. A responsive, newest-updated-first grid of album cards using the existing
   `album-card` and `album-stack` components. Each card has up to three stacked
   stored-photo covers, title, owner name, and “{stored_count} stored of
   {total_count} on Flickr”. A card without a stored cover uses the neutral
   album placeholder. Stored may be zero and may be lower than Flickr total.
4. On a successful zero-result response, replace the grid with the empty block
   and its Browse contacts action.

Do not add a search field, sort control, pagination, bulk selection, download
button, or album editing. Header, shell navigation, and notifications belong to
their own specifications.

## Data and API

No existing OpenAPI operation or backend HTTP route returns the signed-in
user's albums or contact choices. Do not call Flickr from the browser. JOOXF-174
must add the contract requests below before implementation; these are requests,
not current endpoints. The authenticated `/events` operation already exists.

| Element | Source | Fields used |
| --- | --- | --- |
| Album cards and list metadata | `GET /me/albums?contact_nsid={nsid}` **contract request** | `items[]`: `id`, `title`, `owner_nsid`, `owner_name`, `stored_count`, `total_count`, `cover_photo_urls[]`, `updated_at`; response `total`, `updated_at` |
| Contact filter options | Derived from the complete `items[]` album response | Distinct `owner_nsid` and `owner_name` pairs. Include only contacts that own at least one returned album; fall back to the NSID when the name is empty. |
| Stacked covers | `cover_photo_urls[]` in album response | Up to three stored Flickr size URLs ordered by album position. Omit unavailable covers. The server selects stored sizes; the client never constructs Flickr URLs. |
| Live refresh | `GET /events` (exists) | Authenticated SSE. `albums.updated` **event contract request** with optional `contact_nsid`, `total`, and `updated_at`. |

`stored_count` counts this user's stored photo records that belong to the
album; `total_count` is Flickr's `photos_count`. Both are nonnegative integers.
`updated_at` is ISO 8601 UTC. Return all cards for this view; no pagination is
used. Sort by `updated_at` descending, null timestamps last, then `id`
descending. Filter server-side by exact contact NSID. `owner_name` falls back
to `owner_nsid`; an absent cover uses the neutral placeholder. Return only
eligible public or user-visible records, never private observations from
another user. No response may contain Flickr credentials, private media, or
secret values. Do not persist results in browser storage.

Contract requests: add `GET /me/albums`, its response and `contact_nsid` query
filter, and define the `albums.updated` event payload. Contact filter options
are derived from the complete album list, so no contact-list endpoint or
additional interest fields are needed. The album-detail route `/albums/{id}`
belongs to the album view contract and is not added here.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Filter by contact | Native contact select | Set/remove `contact` in the URL, fetch the matching albums, and retain the selection on refresh/back. |
| Open album | Album card | Navigate to `/albums/{id}`; implementation uses the album-detail contract owned by the album view. |
| Browse contacts | Empty-state action | Navigate to `/contacts`. |
| Retry | Error or partial-state button | Refetch the current album list and filter. |

## States

### Normal

The mockup marks the normal result as `data-state="ready"` for the shared UI
gate. Show heading, filter, and cards in response order. Keep the selected
contact visible. A successful zero-item result uses Empty instead.

### Ready

Ready is the shared catalogue's name for Normal. Render the normal state above.

### Loading

For the first request, retain the heading and filter and show six card skeletons
with final card geometry. Hide skeletons from assistive technology and use
static skeletons for reduced motion. During a filter refresh, keep current cards
visible and show a small busy status near the filter; do not replace usable
cards with skeletons.

### Empty

For a successful response with zero albums for All or the selected contact,
show “No albums yet”, “Albums from your contacts will appear here after they
are discovered.”, and “Browse contacts” linking to `/contacts`. Keep the filter
usable. This is a successful result, not an error.

### Error

If the initial list request fails, show “Could not load albums”, “Check your
connection, then try again.”, and “Retry”; do not show an empty state. A 401
uses the shell session-expiry flow. Never display server error text.

### Partial / stale

If a refresh fails with usable data, retain the cards and show “Albums may be
out of date” with “Retry”. On SSE disconnect retain data, expose the shell's
stale indicator, and use the polling fallback described under Live updates.
Show “Updated {relative_time}” when response `updated_at` is more than one
minute old. A failed cover image uses its neutral placeholder and does not
make the list partial.

### Forbidden

For 403, show the standard access-denied state and its shared
`shell.forbidden.title`, `shell.forbidden.text`, and `shell.forbidden.home`
copy (“Go to home”). Clear album and contact-filter data; do not keep stale
cards visible. A filter NSID that
returns 404 is treated as an unavailable contact: reset to All and show the
localized inline notice. Never expose whether an inaccessible contact exists.

## Live updates

This view is live. Reuse the shell's authenticated `/events` stream. On
`albums.updated`, refresh the list when the event has no `contact_nsid`, or when
its `contact_nsid` matches the active filter (including All). Coalesce events
for 500ms; update cards, total and `updated_at` without moving focus or
announcing every changed card. Resume SSE using `Last-Event-ID` as specified by
the shell.

If SSE is unavailable or disconnected for 5 seconds, poll the current list
every 30 seconds while the document is visible. Pause polling while hidden and
refresh once on visibility return. Do not poll faster than 30 seconds. Stop the
timer on route exit. A failed background refresh keeps usable cards and enters
Partial / stale.

## Responsive

- At 1100px and above, use three columns; 700–1099px use two; below 700px use
  one. At 390px, cards fill the content width with 8px gutters. Cover stacks
  preserve their 4:3 aspect ratio and use `object-fit: cover`.
- Filter wraps beneath the heading when needed. Labels and names wrap; no
  horizontal page scrolling. Interactive targets are at least 44px high.
- Validate at 1440px desktop and 390px mobile, at 200% zoom, and in EN/VI.

## Accessibility

- Use one `h1`; associate the visible contact-filter label with a native select.
  Album cards are semantic links with an accessible name including title,
  owner, and stored/total counts.
- Decorative cover images use `alt=""`; the album title names the link. Missing
  covers use the neutral placeholder, never a broken-image glyph. Focus uses
  the 2px accent outline.
- Loading skeletons are hidden from assistive technology. Announce result
  count politely after initial load, not on every live update. Error copy uses
  `role="alert"`; state is never conveyed by color alone. Honor
  `prefers-reduced-motion`.

## i18n

Provide both English (`en`) and Vietnamese (`vi`) values for every view-owned
label, status, message, and accessible name. Contact names, album titles, and
NSIDs are data and are not translated. Shared shell strings remain owned by
`user-shell`.

| Key | EN | VI |
| --- | --- | --- |
| `user.albums.title` | Albums | Album |
| `user.albums.description` | Albums of all your contacts. | Album của tất cả liên hệ của bạn. |
| `user.albums.filter.label` | Filter by contact | Lọc theo liên hệ |
| `user.albums.filter.all` | All contacts | Tất cả liên hệ |
| `user.albums.filter.option` | {contact_name} | {contact_name} |
| `user.albums.card.open` | Open album {title} by {owner_name} | Mở album {title} của {owner_name} |
| `user.albums.card.stored-total` | {stored_count} stored of {total_count} on Flickr | Đã lưu {stored_count} trên tổng số {total_count} trên Flickr |
| `user.albums.empty.title` | No albums yet | Chưa có album |
| `user.albums.empty.body` | Albums from your contacts will appear here after they are discovered. | Album từ các liên hệ sẽ xuất hiện ở đây sau khi được khám phá. |
| `user.albums.empty.action` | Browse contacts | Xem liên hệ |
| `user.albums.error.title` | Could not load albums | Không thể tải album |
| `user.albums.error.body` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.albums.action.retry` | Retry | Thử lại |
| `user.albums.partial.message` | Albums may be out of date | Album có thể chưa được cập nhật |
| `user.albums.updated` | Updated {relative_time} | Cập nhật {relative_time} |
| `user.albums.filter.unavailable` | This contact is unavailable. Showing all contacts. | Không thể truy cập liên hệ này. Đang hiển thị tất cả liên hệ. |
| `user.albums.loading.label` | Loading albums | Đang tải album |

## Test IDs

Stable E2E hooks:

- `data-testid="albums-page"`
- `data-testid="albums-filter"`
- `data-testid="albums-grid"`
- `data-testid="album-card"` (one per card; include album id in a separate
  `data-album-id` attribute)
- `data-testid="albums-loading"`
- `data-testid="albums-empty"`
- `data-testid="albums-error"`
- `data-testid="albums-partial"`
- `data-testid="albums-retry"`
- `data-testid="albums-empty-action"`

## Out of scope

Album detail contents and API, downloading or syncing albums, creating/editing
Flickr albums, album search/sort/pagination, new backend endpoints beyond the
contract requests listed above, and admin views.

## Data

See Data and API above.

## Interactions

See Actions above.

## Test hooks

See Test IDs above; every hook uses the `data-testid` attribute.
