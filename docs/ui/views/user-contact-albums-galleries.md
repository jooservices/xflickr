# Contact: Albums and Galleries tabs

View id `user-contact-albums-galleries`, role USER, light gallery theme (dark
optional). Routes `/contacts/:nsid?tab=albums` and
`/contacts/:nsid?tab=galleries`. Baseline: `vContact()` in the approved
[POC](../poc/xflickr-poc.html), USER role, Albums and Galleries tabs. Shared
rules: [design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-contact-albums-galleries.html](../mockups/user-contact-albums-galleries.html).
Build tickets: JOOXF-173 (shared Contact view), JOOXF-174 (Albums and Galleries
tabs).

## Purpose

Show the public Flickr albums and curated galleries discovered for a contact.
Album cards distinguish photos stored by XFlickr from the total on Flickr;
gallery cards show the number of photos curated from other members. This view is
read-only.

## Route and access

- `/contacts/:nsid?tab=albums` or `?tab=galleries`, role USER. `/contacts/:nsid`
  defaults to Photos. Selecting another Contact tab updates the `tab` query
  without a full page reload; this view selects Albums or Galleries from that
  query.
- `nsid` is a URL-encoded Flickr NSID. Unknown contacts show not found; contacts
  the user may not access show forbidden. Never substitute another contact.
- Require an active user session. An unauthenticated user is sent to
  `/login?reason=expired&return_to=` with the encoded current URL. The server
  derives the XFlickr user from the session; the client sends no user id.
- Title is `Albums by {contact_name} · XFlickr` or
  `Galleries by {contact_name} · XFlickr`. On navigation, focus moves to the
  page `h1`.

## Layout

Inside the USER shell main column (max width 1200px, 24px desktop padding,
16px mobile padding), render in this order:

1. Shared Contact header: Contacts breadcrumb, contact name, avatar, handle and
   depth label.
2. Contact tabs. The selected tab is current and has its loaded result count.
   Counts represent all albums or galleries returned, not visible card count.
3. Albums tab: responsive card grid using the existing `album-card` component.
   Each card shows up to three stacked stored-photo covers, title, contact name,
   and `{stored_count} stored of {total_count}`. If no stored cover is available,
   show the component's neutral album placeholder. `stored_count` may be zero.
4. Galleries tab: responsive cards using the existing `card` component. Each
   shows the gallery title and `{photo_count} photos curated from other
   members`. Galleries have no cover image in this view.

The Albums/Galleries tab content does not add filters, sorting, pagination, or
download controls. Header, shell navigation, and notifications belong to their
own specs.

## Data and API

No existing OpenAPI operation returns a user's contact detail, album list, or
gallery list. Do not call Flickr from the browser. JOOXF-174 must add the
following contract requests to OpenAPI before implementation; use the existing
authenticated `/events` stream for live updates. These responses are scoped to
the signed-in user's permitted public catalog data; never expose another user's
private observations.

| Element | Source | Fields used |
| --- | --- | --- |
| Contact header | `GET /me/contacts/{nsid}` **contract request** (JOOXF-173) | `nsid`, `username`, `real_name`, `avatar_url`, `depth` |
| Album tab | `GET /me/contacts/{nsid}/albums` **contract request** | `items[]`: `id`, `title`, `owner_name`, `stored_count`, `total_count`, `cover_photo_urls[]`; `total`, `updated_at` |
| Gallery tab | `GET /me/contacts/{nsid}/galleries` **contract request** | `items[]`: `id`, `title`, `photo_count`; `total`, `updated_at` |
| Cover images | `cover_photo_urls[]` in album response | Up to three stored Flickr size URLs, ordered by album position; absent when no stored cover is available. Server selects URLs from stored sizes. |
| Live refresh | `GET /events` (exists) | Authenticated SSE; `contact.albums.updated` or `contact.galleries.updated` **event contract requests**, each with `nsid`, `total`, `updated_at` |

`stored_count` is the number of album membership photos stored for this user's
view; `total_count` is Flickr's album `photos_count`. Both counts are nonnegative
integers and are independent: stored can be less than total. `photo_count` is
the Flickr curated count for the gallery, including photos owned by other
members; it is not an XFlickr stored-photo count. Album ordering is most recently
updated first, then stable descending id; gallery ordering is most recently
updated first, then stable descending id. `updated_at` is an ISO 8601 UTC
timestamp for the catalog snapshot. The endpoint returns all cards for this
contact; no pagination is used in this view.

Do not persist responses in browser storage or construct Flickr API URLs. A
missing avatar uses the shared initials fallback. Broken or missing album cover
URLs use the neutral placeholder. Never show a missing value as `undefined`;
zero counts remain visible.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Change Contact tab | Contact tab control | Navigate to the selected Contact section, preserving `nsid`. |
| Open album | Album card | Navigate to `/albums/{id}`. This uses the existing album detail route; no album-detail API is introduced here. |
| Browse contacts | Empty-state CTA | Navigate to `/contacts`. |
| Retry | Error or partial-state retry button | Refetch only the failed contact or selected-tab resource. |

Gallery cards are not interactive: the POC presents them as informational cards
and this ticket does not define a gallery detail destination. Album cards are
one keyboard target each. Do not add download, edit, delete, or Flickr actions.

## States

### Ready

This is the normal state shown by the mockup's `data-state="ready"` sample.
Show the shared header, selected tab and cards in server order. The Albums count
and stored/total figures use the response values exactly. Galleries show the
curated count. Announce the result count once after initial load; a live refresh
does not steal focus or announce every changed card.

### Loading

For initial load, show the known Contact header if available, selected tab, and
four card skeletons with the component's reduced-motion behavior. If header data
is also loading, show its skeleton. Keep usable cards visible during background
refresh and show a small inline busy status. Skeletons are hidden from
assistive technology.

### Empty

For a successful zero-item response, show the empty block and CTA below the
header and selected tab. Albums: “No albums” / “This contact has no public
albums.” Galleries: “No galleries” / “Galleries are curated sets of other
people’s photos. This contact has none.” CTA: “Browse contacts”. Hide the card
grid. Empty is a successful result, not an error.

### Error

When initial contact or selected-tab loading fails, show “Could not load albums”
or “Could not load galleries”, “Check your connection, then try again.”, and
“Retry”. Keep independently loaded header data; do not show an empty state.
Never display server error text. A 401 follows shell session-expiry handling.

### Partial / stale

If the contact header succeeds but the selected tab fails, retain the header and
show an inline warning in the content area: “Some albums may be out of date” or
“Some galleries may be out of date”, with “Retry”. If there is prior usable tab
data, keep showing it. On an SSE disconnect, retain data, use the shell stale
indicator, and switch to polling. Show “Updated {relative_time}” when data is
older than one minute. A refresh failure never replaces usable cards with
skeletons.

### Forbidden

For 403 show the standard access-denied state; for 404 show the standard
contact-not-found state. Both include “Back to contacts”. Clear contact and tab
data after either response. Do not keep stale cards visible.

## Live updates

Both tabs are live. Subscribe on mount to `/events`; filter
`contact.albums.updated` or `contact.galleries.updated` by exact `nsid` and
selected tab. Coalesce matching events for 500ms, then refetch that selected-tab
endpoint and update its total and `updated_at`. Keep current focus. Reconnect SSE
with `Last-Event-ID` as the shell specifies. If SSE is unavailable or has been
disconnected for 5 seconds, poll the selected endpoint every 30 seconds while
the tab is visible; pause while hidden and refetch on visibility. Never poll
faster than every 30 seconds. Stop polling and unsubscribe on navigation.

## Responsive

- At 900px and above, use three columns when space permits; below 900px use two
  columns and below 600px use one column. At 390px, cards fill the content width
  with 8px grid gutters. The album cover stack keeps its aspect ratio and crops
  with `object-fit: cover`.
- Contact header wraps. Contact tabs scroll horizontally without shrinking
  labels. Long names and Vietnamese labels wrap without clipping.
- Validate at 1440px desktop and 390px mobile, at 200% zoom, and in EN/VI. The
  page must not scroll horizontally; only the tab strip may scroll horizontally.

## Accessibility

- Use one `h1`; breadcrumb navigation is a labelled `nav`. Contact sections use
  the shared tabs behavior and expose the selected section as current.
- Album cards are semantic links with an accessible name including album title
  and stored/total count. Gallery cards are informational `article` elements,
  not focusable controls. Decorative stacked cover images use empty alt text;
  the album title names the card.
- Use semantic buttons and links. Visible focus follows the design-system 2px
  accent outline. Empty and error icons are decorative; error copy uses
  `role="alert"`. Announce initial item counts politely, not each SSE refresh.
- Skeletons are hidden from assistive technology. State is never conveyed by
  color alone. Honor `prefers-reduced-motion`.

## i18n

English (`en`) and Vietnamese (`vi`) values for every string introduced by this
view. Shared shell strings remain owned by `user-shell`; names and Flickr album
and gallery titles are not translated.

| Key | EN | VI |
| --- | --- | --- |
| `user.contact-albums-galleries.title.albums` | Albums by {contact_name} | Album của {contact_name} |
| `user.contact-albums-galleries.title.galleries` | Galleries by {contact_name} | Bộ sưu tập của {contact_name} |
| `user.contact-albums-galleries.breadcrumb.contacts` | Contacts | Liên hệ |
| `user.contact-albums-galleries.contact.depth` | Depth {depth} | Cấp độ {depth} |
| `user.contact-albums-galleries.tabs.label` | Contact sections | Các mục liên hệ |
| `user.contact-albums-galleries.tabs.photos` | Photos | Ảnh |
| `user.contact-albums-galleries.tabs.favorites` | Favorites | Yêu thích |
| `user.contact-albums-galleries.tabs.albums` | Albums | Album |
| `user.contact-albums-galleries.tabs.galleries` | Galleries | Bộ sưu tập |
| `user.contact-albums-galleries.tabs.groups` | Groups | Nhóm |
| `user.contact-albums-galleries.tabs.about` | About | Giới thiệu |
| `user.contact-albums-galleries.album.stored-total` | {stored_count} stored of {total_count} | Đã lưu {stored_count} trên tổng số {total_count} |
| `user.contact-albums-galleries.gallery.photo-count` | {photo_count} photos curated from other members | {photo_count} ảnh được tuyển chọn từ thành viên khác |
| `user.contact-albums-galleries.empty.albums.title` | No albums | Không có album |
| `user.contact-albums-galleries.empty.albums.text` | This contact has no public albums. | Liên hệ này không có album công khai. |
| `user.contact-albums-galleries.empty.galleries.title` | No galleries | Không có bộ sưu tập |
| `user.contact-albums-galleries.empty.galleries.text` | Galleries are curated sets of other people’s photos. This contact has none. | Bộ sưu tập là các nhóm ảnh được tuyển chọn từ người khác. Liên hệ này chưa có bộ sưu tập nào. |
| `user.contact-albums-galleries.empty.cta` | Browse contacts | Xem danh sách liên hệ |
| `user.contact-albums-galleries.error.albums.title` | Could not load albums | Không thể tải album |
| `user.contact-albums-galleries.error.galleries.title` | Could not load galleries | Không thể tải bộ sưu tập |
| `user.contact-albums-galleries.error.text` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.contact-albums-galleries.error.retry` | Retry | Thử lại |
| `user.contact-albums-galleries.partial.albums` | Some albums may be out of date | Một số album có thể chưa được cập nhật |
| `user.contact-albums-galleries.partial.galleries` | Some galleries may be out of date | Một số bộ sưu tập có thể chưa được cập nhật |
| `user.contact-albums-galleries.stale.updated` | Updated {relative_time} | Đã cập nhật {relative_time} |
| `user.contact-albums-galleries.forbidden.title` | You cannot view this contact | Bạn không thể xem liên hệ này |
| `user.contact-albums-galleries.not-found.title` | Contact not found | Không tìm thấy liên hệ |
| `user.contact-albums-galleries.forbidden.back` | Back to contacts | Quay lại danh sách liên hệ |
| `user.contact-albums-galleries.loading.albums` | Loading albums | Đang tải album |
| `user.contact-albums-galleries.loading.galleries` | Loading galleries | Đang tải bộ sưu tập |
| `user.contact-albums-galleries.loading.count` | {count} items | {count} mục |
| `user.contact-albums-galleries.album.open` | Open album {title}, {stored_count} stored of {total_count} | Mở album {title}, đã lưu {stored_count} trên tổng số {total_count} |
| `user.contact-albums-galleries.live.updated` | Updated | Đã cập nhật |

## Test IDs

E2E selectors use stable `data-testid` values independent of locale and theme:

- `user-contact-albums-galleries-page`
- `user-contact-albums-galleries-tabs`
- `user-contact-albums-galleries-albums-grid`
- `user-contact-albums-galleries-album-{album_id}`
- `user-contact-albums-galleries-galleries-grid`
- `user-contact-albums-galleries-gallery-{gallery_id}`
- `user-contact-albums-galleries-loading`
- `user-contact-albums-galleries-empty`
- `user-contact-albums-galleries-error`
- `user-contact-albums-galleries-partial`
- `user-contact-albums-galleries-forbidden`
- `user-contact-albums-galleries-retry`
- `user-contact-albums-galleries-browse-contacts`
- `user-contact-albums-galleries-live-status`

## Out of scope

Album detail implementation, gallery detail, photo grids, favorites, groups,
download, edit/delete, filters, sorting, pagination, API implementation, and
OpenAPI edits. The endpoints and events above are contract requests for
JOOXF-174, not changes in this design ticket.

## Interactions

Compatibility index for the UI-spec gate: user actions are defined in
[Actions](#actions).

## Data

Compatibility index for the UI-spec gate: sources and fields are defined in
[Data and API](#data-and-api).

## Test hooks

Compatibility index for the UI-spec gate: stable selectors are listed in
[Test IDs](#test-ids).
