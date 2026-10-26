# Contact: Favorites tab

View id `user-contact-favorites`, role USER, light gallery theme (dark
optional). Route `/contacts/:nsid`, selected tab `favorites`. Baseline:
`vContact()` in the approved [POC](../poc/xflickr-poc.html), USER role,
Favorites tab. Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md). Mockup:
[user-contact-favorites.html](../mockups/user-contact-favorites.html). Build
ticket: JOOXF-173. This tab renders inside the Contact view and USER shell.

## Purpose

Show the photos this contact has favorited and make the discovery consequence
clear: each photo's owner is a candidate contact one depth below the current
contact. The signed-in user's configured depth limit is authoritative; owners
beyond that limit are not discovered as contacts.

## Route and access

- `/contacts/:nsid?tab=favorites`, role USER. `/contacts/:nsid` defaults to the
  Photos tab; this tab is selected by the `tab=favorites` query. Selecting
  another Contact tab updates the query without a full page reload.
- `nsid` is the Flickr NSID, URL encoded. Unknown or inaccessible contacts show
  the not-found/forbidden shell result; the page never substitutes another
  contact.
- Require an active user session. Unauthenticated users are sent to
  `/login?reason=expired&return_to=` with the encoded current URL. The server
  derives the XFlickr user from the session; the client sends no user id.
- Document title `Favorites by {contact_name} · XFlickr`. On navigation, focus
  moves to the page `h1`.

## Layout

Inside the USER shell main column (max width 1200px, 24px desktop padding,
16px mobile padding), in this order:

1. Contact header shared with the Contact view: breadcrumb to Contacts, contact
   name, avatar, handle, and depth label.
2. Contact tabs. Favorites is `aria-current`/pressed; its count is the loaded
   favorite total supplied by the API, not just the current page length.
3. A muted explanatory line: owners shown here are candidates at depth
   `contact.depth + 1`. If that depth is at or below the user's effective
   `depth` limit, say they are discovered at that depth. If it exceeds the
   limit, say the limit prevents their discovery; do not imply they become
   contacts or will be crawled.
4. Photo grid using the existing `photo-grid` / `photo-card` components. Each
   card has the photo image and concise title; its owner name and avatar link
   to `/contacts/{owner_nsid}`. A photo click opens the existing lightbox; the
   owner link remains a distinct keyboard target. The grid appends pages with
   a Load more button when `next_cursor` is present.

No download, star, crawl-priority, filter, sort, or bulk action is added to
this tab. Theme, shell navigation and notification controls belong to the
shell specification.

## Data and API

No current OpenAPI operation returns a contact detail or its favorited photo
grid. Do not call Flickr from the browser. Build this view against the
following contract requests for JOOXF-173; the build ticket must add them to
OpenAPI before implementation. Existing `GET /me/crawl/settings` supplies
the depth values. Existing `GET /events` is the authenticated SSE connection.

| Element | Source | Fields used |
| --- | --- | --- |
| Contact header and depth | `GET /me/contacts/{nsid}` **contract request** | `nsid`, `username`, `real_name`, `avatar_url`, `depth` (this user's minimum interest depth) |
| Favorites grid | `GET /me/contacts/{nsid}/favorites?cursor=&limit=30` **contract request** | `items[]`: `id`, `title`, `thumbnail_url`, `owner_nsid`, `owner_name`, `owner_avatar_url`, `taken_at`; `total`, `next_cursor`, `updated_at` |
| User depth limit | `GET /me/crawl/settings` (exists) | `depth` (effective depth for this user) |
| Live refresh | `GET /events` (exists) | authenticated SSE; `contact.favorites.updated` **event contract request** with `nsid`, `total`, `updated_at` |

The favorites endpoint is user scoped. It returns only photos this user's
connected Flickr account can see under Flickr visibility rules. It must not
expose private records observed only by another user. `thumbnail_url` is a
stored Flickr size URL selected by the server; the browser does not request
Flickr credentials or construct a Flickr API URL. Missing/unavailable photo
images use the shared neutral photo placeholder. `owner_nsid` is stable; names
and avatar URLs may be absent and use the shared initials avatar fallback.

Contract request response shape for the favorites page:

```json
{
  "items": [{
    "id": "52000000001",
    "title": "Rain over the old quarter",
    "thumbnail_url": "https://stored-size-url",
    "owner_nsid": "48291037@N04",
    "owner_name": "Linh Tran",
    "owner_avatar_url": null,
    "taken_at": "2026-10-20T08:30:00Z"
  }],
  "total": 18,
  "next_cursor": null,
  "updated_at": "2026-10-26T03:00:00Z"
}
```

The example is illustrative only. Do not persist the response in browser storage.
The user's crawl setting bounds discovery: if `contact.depth + 1 > depth`, the
owners remain visible as photo owners but are not discovered as contacts. Show
the limit explanation and do not present a control to raise it here. Existing
crawl rules retain already discovered records when a depth limit is lowered.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Open owner contact | Owner name/avatar link on a photo | Navigate to `/contacts/{owner_nsid}` and default that contact view to Photos. |
| Open photo | Photo tile, excluding the owner link | Open the shared lightbox for that photo; close returns focus to the tile. |
| Change contact tab | Contact tab | Navigate to the selected tab, preserving the contact NSID. |
| Load more | End of grid | Request the next cursor page and append without moving focus; hide when `next_cursor` is null. |
| Retry | Error banner button | Retry only the failed request. |
| Browse contacts | Empty-state CTA | Navigate to `/contacts`. |

## States

### Ready

Normal state: show the header, depth explanation and favorite grid. The depth
explanation says owners are discovered at `contact.depth + 1` when that value
is within the user's effective limit; otherwise it says the limit prevents
their discovery as contacts. Announce
the grid total once; do not announce each image during a live refresh. Preserve
the current cursor page when a refresh updates the total. Updated or newly
inserted items follow the endpoint's stable ordering and do not steal focus.

### Loading

For the initial request show the contact header skeleton, explanation-line
skeleton, and six photo-card skeletons. Keep the selected Favorites tab visible
when the contact header is already known. For Load more, keep existing photos
visible and show a compact inline skeleton in the grid; disable the button while
that page is in flight. Respect reduced motion by rendering static skeletons.

### Empty

When the first page succeeds with `total: 0`, show the Favorites empty block
below the explanation: “No favorites yet” and “Photos this contact favorites
will appear here after XFlickr crawls them.” CTA: “Browse contacts”. Hide Load
more. Empty means a successful zero result, not a failed request.

### Error

If initial contact or favorites loading fails, show “Could not load favorites”
and “Check your connection, then try again.” with “Retry”. Keep any already
loaded header data; show no empty state. If a later page fails, retain current
photos and show an inline retry for that page. A 401 follows shell session
expiry handling; other errors use this state. Never display server error text.

### Partial / stale

If one resource fails while the other loaded, render the successful resource and
place a warning banner in the failed resource's location with “Some favorites
may be out of date” and “Retry”. If the SSE connection drops, retain the last
successful data, show the shell's offline/stale indicator, and switch to
polling. Show `updated_at` as “Updated {relative_time}” when older than one
minute. A refresh failure does not replace usable data with skeletons.

### Forbidden

For 403, show the standard access denied state and a “Back to contacts” link.
For 404, show the standard contact-not-found state and the same link. Do not
render stale contact or photo data after either response.

## Live updates

This view is live. Subscribe on mount to the existing `/events` stream and
filter the contract-requested `contact.favorites.updated` event by exact
`nsid`. On a matching event, coalesce events for 500ms, refetch the first page
and total, and discard any later pages so the grid cannot combine results from
different snapshots. Keep focus where it is and do not announce refreshed
items. Use the server `updated_at` for freshness. Reconnect SSE with
`Last-Event-ID` as the shell specifies. If SSE is unavailable or disconnected
for 5 seconds, poll
`GET /me/contacts/{nsid}/favorites` every 30 seconds while this tab is visible;
pause polling while hidden and refetch on visibility. Stop the timer and
unsubscribe on navigation. Never poll faster than 30 seconds.

## Responsive

- At 900px and above use four grid columns; 600–899px use three; below 600px
  use two columns. At 390px keep the two-column photo grid and at least 8px
  gutters; image tiles preserve their returned aspect ratio and crop with
  `object-fit: cover`.
- Contact header wraps; the tabs scroll horizontally without shrinking their
  labels. At narrow widths reduce shell padding to 16px and let the depth
  explanation wrap. The owner link remains visible under each image.
- Validate at 1440px desktop and 390px mobile, at 200% zoom, and with EN/VI
  labels. No horizontal page scrolling; the tab strip may scroll horizontally.

## Accessibility

- Use one `h1`; `nav` for contact tabs with `aria-label="Contact sections"`;
  selected tab exposes `aria-current="page"`. The grid is a labelled list.
- Photo alt text uses the photo title; decorative thumbnails in repeated
  contexts may use empty alt only when the adjacent card text names the photo.
  Owner links have the accessible name “View {owner_name} contact”.
- Photo tile and owner link are separate focus targets. Use semantic anchors and
  buttons; Enter opens a link or photo, Escape closes the lightbox and restores
  focus. Focus indicator follows the design-system 2px accent outline.
- Announce initial result count and explicit load-more completion in a polite
  live region. Do not announce every SSE refresh. Errors use `role="alert"`;
  skeletons are hidden from assistive technology. Do not use color alone for
  state. Honor `prefers-reduced-motion`.

## i18n

English (`en`) and Vietnamese (`vi`) values are listed here for every string
introduced by this tab. Shared shell strings remain owned by `user-shell`.
Names, photo titles and dates from Flickr are not translated.

| Key | EN | VI |
| --- | --- | --- |
| `user.contact-favorites.title` | Favorites by {contact_name} | Ảnh yêu thích của {contact_name} |
| `user.contact-favorites.breadcrumb.contacts` | Contacts | Liên hệ |
| `user.contact-favorites.contact.depth` | Depth {depth} | Cấp độ {depth} |
| `user.contact-favorites.tabs.photos` | Photos | Ảnh |
| `user.contact-favorites.tab` | Favorites | Yêu thích |
| `user.contact-favorites.tabs.albums` | Albums | Album |
| `user.contact-favorites.tabs.galleries` | Galleries | Bộ sưu tập |
| `user.contact-favorites.tabs.groups` | Groups | Nhóm |
| `user.contact-favorites.tabs.about` | About | Giới thiệu |
| `user.contact-favorites.depth.within-limit` | Owners of these photos are discovered as depth {depth} contacts. | Chủ sở hữu của những ảnh này được phát hiện ở cấp độ liên hệ {depth}. |
| `user.contact-favorites.depth.limit` | Owners of these photos would be depth {depth} contacts, but your depth limit prevents their discovery. | Chủ sở hữu của những ảnh này sẽ là liên hệ cấp độ {depth}, nhưng giới hạn độ sâu ngăn việc phát hiện họ. |
| `user.contact-favorites.photo-owner` | View {owner_name} contact | Xem liên hệ {owner_name} |
| `user.contact-favorites.empty.title` | No favorites yet | Chưa có ảnh yêu thích |
| `user.contact-favorites.empty.text` | Photos this contact favorites will appear here after XFlickr crawls them. | Ảnh người này yêu thích sẽ xuất hiện tại đây sau khi XFlickr thu thập dữ liệu. |
| `user.contact-favorites.empty.cta` | Browse contacts | Xem danh sách liên hệ |
| `user.contact-favorites.error.title` | Could not load favorites | Không thể tải ảnh yêu thích |
| `user.contact-favorites.error.text` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.contact-favorites.error.retry` | Retry | Thử lại |
| `user.contact-favorites.partial.title` | Some favorites may be out of date | Một số ảnh yêu thích có thể chưa được cập nhật |
| `user.contact-favorites.partial.retry` | Retry | Thử lại |
| `user.contact-favorites.stale.updated` | Updated {relative_time} | Đã cập nhật {relative_time} |
| `user.contact-favorites.forbidden.title` | You cannot view this contact | Bạn không thể xem liên hệ này |
| `user.contact-favorites.not-found.title` | Contact not found | Không tìm thấy liên hệ |
| `user.contact-favorites.forbidden.back` | Back to contacts | Quay lại danh sách liên hệ |
| `user.contact-favorites.loading.label` | Loading favorites | Đang tải ảnh yêu thích |
| `user.contact-favorites.loading.more` | Loading more photos | Đang tải thêm ảnh |
| `user.contact-favorites.grid.label` | Favorited photos | Ảnh đã yêu thích |
| `user.contact-favorites.grid.count` | {count, plural, one {# favorite photo} other {# favorite photos}} | {count} ảnh yêu thích |
| `user.contact-favorites.load-more` | Load more | Tải thêm |
| `user.contact-favorites.load-more.done` | More favorites loaded | Đã tải thêm ảnh yêu thích |
| `user.contact-favorites.tabs.label` | Contact sections | Các mục liên hệ |
| `user.contact-favorites.updated` | Updated | Đã cập nhật |

## Test IDs

E2E selectors use the `data-testid` attribute and are stable across locale and theme:

- `data-testid="user-contact-favorites-page"`
- `data-testid="user-contact-favorites-tabs"`
- `data-testid="user-contact-favorites-depth-note"`
- `data-testid="user-contact-favorites-grid"`
- `data-testid="user-contact-favorites-photo-{photo_id}"`
- `data-testid="user-contact-favorites-owner-{photo_id}"`
- `data-testid="user-contact-favorites-loading"`
- `data-testid="user-contact-favorites-empty"`
- `data-testid="user-contact-favorites-error"`
- `data-testid="user-contact-favorites-partial"`
- `data-testid="user-contact-favorites-forbidden"`
- `data-testid="user-contact-favorites-retry"`
- `data-testid="user-contact-favorites-load-more"`
- `data-testid="user-contact-favorites-live-status"`

## Out of scope

Favorites management (add/remove), photo download/tray controls, filters,
sorting, discovery-depth editing, contact crawl controls, API implementation,
OpenAPI edits, and other Contact tabs. The required contract additions are
requests for the build ticket, not changes in this design ticket.

## Data

Compatibility index for the existing UI-spec gate: data sources and fields are
defined in [Data and API](#data-and-api).

## Interactions

Compatibility index for the existing UI-spec gate: actions are defined in
[Actions](#actions).

## Test hooks

Compatibility index for the existing UI-spec gate: selectors are defined in
[Test IDs](#test-ids).
