# Contact: Groups and About tabs

View id `user-contact-groups-about`, role USER, light gallery theme (dark
optional). Routes `/contacts/:nsid?tab=groups` and
`/contacts/:nsid?tab=about`. Baseline: `vContact()` in the approved
[POC](../poc/xflickr-poc.html), USER role. Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md). Mockup:
[user-contact-groups-about.html](../mockups/user-contact-groups-about.html).
Build ticket: JOOXF-173.

## Purpose

Show public Flickr groups associated with a contact and the crawl and discovery
metadata XFlickr has for that contact. Group pool crawling is an explicit
per-group opt-in and starts off for every group.

## Route and access

- USER role; require an active session. Unauthenticated users go to
  `/login?reason=expired&return_to=` with the encoded current URL. The server
  derives the XFlickr user from the session; the client sends no user id.
- `/contacts/:nsid?tab=groups` selects Groups; `?tab=about` selects About.
  `/contacts/:nsid` defaults to Photos. Tab navigation updates the query and
  preserves the URL-encoded Flickr NSID.
- Unknown contacts show not found; inaccessible contacts show forbidden. Never
  substitute another contact or retain data after 403/404.
- Title is `{tab} · {contact_name} · XFlickr`. Move focus to the page `h1` after
  route navigation.

## Layout

Use the shared Contact header inside the USER shell main column (max width
1200px, 24px desktop padding, 16px mobile padding): Contacts breadcrumb,
contact name, avatar, Flickr handle and depth. Follow it with shared contact
tab navigation. Mark the current tab with `aria-current="page"`; show the
loaded item count on Groups only.

Groups renders a responsive semantic table with Group, Members and Pool crawl
columns. Each row shows the Flickr group name, member count and a labelled
switch. The switch communicates On or Off; it is Off initially. On mobile,
reflow each row as a labelled group summary without clipping or hiding data.

About renders a definition list for Flickr NSID, Location, Found via, Depth and
Last change check; beneath it, show facet progress and tombstone counts. Facet
progress is a compact table with facet, completed/total or status, and last
updated time. Include photos, favorites, albums, galleries and groups. The
tombstone summary lists unavailable photo count and last update. Use existing
card, table, switch, badge and progress components; do not add components.

## Data and API

No current OpenAPI operation returns contact details, group crawl preferences,
facet status or tombstone summaries. Do not call Flickr from the browser.
JOOXF-173 must add the following contract requests to OpenAPI before
implementation. Data is scoped to the signed-in user's permitted catalog and
Flickr visibility; never reveal private records observed only by another user.

| View data | Source | Fields used |
| --- | --- | --- |
| Contact header and About metadata | `GET /me/contacts/{nsid}` **contract request** | `nsid`, `username`, `real_name`, `avatar_url`, `location`, `depth`, `found_via[]`, `last_change_check_at` |
| Group rows and preference | `GET /me/contacts/{nsid}/groups` **contract request** | `items[]`: `nsid`, `name`, `members_count`, `pool_crawl_enabled`; `total`, `updated_at` |
| Set pool crawl | `PUT /me/contacts/{nsid}/groups/{group_nsid}/pool-crawl` **contract request** | request `enabled`; response `enabled`, `changed_at` |
| Facet progress | `GET /me/contacts/{nsid}/about` **contract request** | `facets[]`: `name`, `completed`, `total`, `status`, `updated_at`; `tombstones.count`, `tombstones.updated_at`; `updated_at` |
| Live refresh | `GET /events` (exists) | authenticated SSE; `contact.groups.updated` and `contact.about.updated` event **contract requests** with `nsid`, `updated_at` |

`found_via[]` is the ordered discovery chain from the root connected account to
this contact, with `nsid` and a display label per link. `depth` is this user's
minimum interest depth, not a global crawl depth. `last_change_check_at` may be
null before a check completes. Facet counts are nonnegative; `total` may be
null if Flickr provides none. Status is `pending`, `running`, `complete`,
`partial` or `failed`; pair status colors with text. Tombstones are photos
Flickr reported unavailable or permission denied for, excluded from available
photo counts. Missing time or location displays localized “Not available,”
never `undefined`. Group preference is per XFlickr user and group, defaults to
`false`, and enables pool crawling only for that group; it does not alter
XFlickr sharing settings. Failed saves restore the last confirmed value.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Change Contact tab | Contact tab link | Navigate to the selected section while preserving `nsid`. |
| Set group pool crawl | Group switch | Send `PUT .../pool-crawl` with `enabled`; optimistically update, disable while saving, and roll back with an error announcement on failure. |
| Browse contacts | Empty, forbidden or not-found action | Navigate to `/contacts`. |
| Retry | Error or partial-state retry button | Refetch only the failed resource. |

No group join/leave, pool photo browsing, search, or bulk toggle is defined.
Pool crawl is explicitly opt-in for each group.

## States

### Ready

Show the shared header, selected tab and current data. Groups show each
preference value from the API. About shows metadata, all five facet rows and
the tombstone summary. Null values use localized fallback copy. Do not infer
completion from a missing count.

### Loading

Show known Contact header data when available and skeletons matching the table
or About card. Mark skeletons `aria-hidden="true"`; announce “Loading groups”
or “Loading contact details” once via a polite live region. Disable switches
until their preference is known. During background refresh, keep data visible
and show a small busy status.

### Empty

Groups copy: “No groups found” and “This contact has no public Flickr groups.”
CTA “Browse contacts”. About is not an empty list: render metadata rows with
“Not available” fallbacks. If its detail resource has no record, show
not-found. Do not show a blank card.

### Error

For a failed Groups or About request, show “Could not load groups” or “Could
not load contact details”, then “Check your connection, then try again.”
Provide Retry. A failed switch save restores the confirmed value and announces
“Could not update pool crawl. Try again.”

### Partial and stale

If one facet or preference failed, retain successful rows, mark the failed item
“Could not load” and offer Retry for it. For stale data, keep it visible, show
its last-updated time, and label it “Some contact details may be out of date”
or “Some groups may be out of date.” On reconnect, refresh affected resources.

### Forbidden

For 401, follow the expired-session route. For 403, remove contact data and
show “You cannot view this contact” with “Back to contacts.” For 404, show
“Contact not found” and the same action. Do not automatically retry either.

## Live updates

This view is live while visible. Connect to authenticated `GET /events`, resume
with `Last-Event-ID`, and match `contact.groups.updated` or
`contact.about.updated` by exact `nsid`. Refresh only the matching resource;
never steal focus. If SSE disconnects, poll the selected resource every 30
seconds while visible; pause polling in a hidden tab and reconnect SSE with
exponential backoff capped at 60 seconds. On reconnect, refresh once, then use
events. Announce one concise “Updated” status per completed refresh. Show a
disconnected/stale indicator until successful refresh.

## Responsive

At 1440px use the 1200px main column and full-width table/card. At 390px use
16px shell gutters, stack About sections, and reflow group rows with column
labels. Allow EN/VI text to expand 30%; wrap names, locations and found-via
links. Switches are at least 44px high; prevent horizontal page overflow. Hide
no fields or actions at narrow widths.

## Accessibility

Use one `h1`, breadcrumb navigation, labelled tab navigation, and semantic
table headers with `scope="col"`. Group switches use native checkbox/switch
semantics, accessible name “Pool crawl for {group_name}”, and checked state
matching confirmed server data. Every control is keyboard reachable with the
design-system focus outline. Pair status color with text/icon. Use
`aria-live="polite"` for load, save, refresh and error announcements; do not
announce skeletons. Respect reduced motion.

## i18n

English is source copy. Vietnamese uses formal-neutral wording and preserves
Flickr, XFlickr, IDs, NSIDs, API and SSE. These are all view-owned visible copy
keys; shared shell and common button strings use existing catalog keys.

| Key | EN | VI |
| --- | --- | --- |
| `user.contact-groups-about.title.groups` | Groups · {contact_name} · XFlickr | Nhóm · {contact_name} · XFlickr |
| `user.contact-groups-about.title.about` | About · {contact_name} · XFlickr | Giới thiệu · {contact_name} · XFlickr |
| `user.contact-groups-about.breadcrumb.contacts` | Contacts | Liên hệ |
| `user.contact-groups-about.tabs.label` | Contact sections | Các mục liên hệ |
| `user.contact-groups-about.tabs.groups` | Groups | Nhóm |
| `user.contact-groups-about.tabs.about` | About | Giới thiệu |
| `user.contact-groups-about.groups.table` | Flickr groups | Nhóm Flickr |
| `user.contact-groups-about.groups.count` | {count, plural, one {# group} other {# groups}} | {count} nhóm |
| `user.contact-groups-about.groups.name` | Group | Nhóm |
| `user.contact-groups-about.groups.members` | Members | Thành viên |
| `user.contact-groups-about.groups.pool-crawl` | Pool crawl | Thu thập ảnh trong nhóm |
| `user.contact-groups-about.groups.enabled` | On | Bật |
| `user.contact-groups-about.groups.disabled` | Off | Tắt |
| `user.contact-groups-about.groups.off-default` | Off by default | Mặc định là tắt |
| `user.contact-groups-about.groups.toggle` | Pool crawl for {group_name} | Thu thập ảnh trong nhóm {group_name} |
| `user.contact-groups-about.about.nsid` | Flickr NSID | NSID Flickr |
| `user.contact-groups-about.about.location` | Location | Vị trí |
| `user.contact-groups-about.about.found-via` | Found via | Được tìm thấy qua |
| `user.contact-groups-about.about.depth` | Depth | Độ sâu |
| `user.contact-groups-about.about.last-check` | Last change check | Lần kiểm tra thay đổi gần nhất |
| `user.contact-groups-about.about.facets` | Crawl progress | Tiến độ thu thập |
| `user.contact-groups-about.about.facet` | Facet | Loại dữ liệu |
| `user.contact-groups-about.facet.photos` | Photos | Ảnh |
| `user.contact-groups-about.facet.favorites` | Favorites | Yêu thích |
| `user.contact-groups-about.facet.albums` | Albums | Album |
| `user.contact-groups-about.facet.galleries` | Galleries | Bộ sưu tập |
| `user.contact-groups-about.facet.groups` | Groups | Nhóm |
| `user.contact-groups-about.about.progress` | Progress | Tiến độ |
| `user.contact-groups-about.about.status` | Status | Trạng thái |
| `user.contact-groups-about.status.pending` | Pending | Đang chờ |
| `user.contact-groups-about.status.running` | Running | Đang chạy |
| `user.contact-groups-about.status.complete` | Complete | Hoàn tất |
| `user.contact-groups-about.status.partial` | Partial | Một phần |
| `user.contact-groups-about.status.failed` | Failed | Thất bại |
| `user.contact-groups-about.about.updated` | Updated | Đã cập nhật |
| `user.contact-groups-about.about.tombstones` | Unavailable photos | Ảnh không còn khả dụng |
| `user.contact-groups-about.value.unavailable` | Not available | Không có thông tin |
| `user.contact-groups-about.groups.empty.title` | No groups found | Không tìm thấy nhóm |
| `user.contact-groups-about.groups.empty.text` | This contact has no public Flickr groups. | Liên hệ này không có nhóm Flickr công khai. |
| `user.contact-groups-about.empty.cta` | Browse contacts | Xem danh sách liên hệ |
| `user.contact-groups-about.groups.loading` | Loading groups | Đang tải nhóm |
| `user.contact-groups-about.about.loading` | Loading contact details | Đang tải thông tin liên hệ |
| `user.contact-groups-about.groups.error.title` | Could not load groups | Không thể tải nhóm |
| `user.contact-groups-about.about.error.title` | Could not load contact details | Không thể tải thông tin liên hệ |
| `user.contact-groups-about.error.text` | Check your connection, then try again. | Kiểm tra kết nối rồi thử lại. |
| `user.contact-groups-about.error.retry` | Retry | Thử lại |
| `user.contact-groups-about.groups.update-error` | Could not update pool crawl. Try again. | Không thể cập nhật việc thu thập ảnh trong nhóm. Hãy thử lại. |
| `user.contact-groups-about.partial.groups` | Some groups may be out of date | Một số nhóm có thể chưa được cập nhật |
| `user.contact-groups-about.partial.about` | Some contact details may be out of date | Một số thông tin liên hệ có thể chưa được cập nhật |
| `user.contact-groups-about.partial.item-error` | Could not load | Không thể tải |
| `user.contact-groups-about.stale.updated` | Updated {relative_time} | Đã cập nhật {relative_time} |
| `user.contact-groups-about.forbidden.title` | You cannot view this contact | Bạn không thể xem liên hệ này |
| `user.contact-groups-about.not-found.title` | Contact not found | Không tìm thấy liên hệ |
| `user.contact-groups-about.forbidden.back` | Back to contacts | Quay lại danh sách liên hệ |
| `user.contact-groups-about.live.updated` | Updated | Đã cập nhật |
| `user.contact-groups-about.live.disconnected` | Live updates paused; showing saved data | Đã tạm dừng cập nhật trực tiếp; đang hiển thị dữ liệu đã lưu |

## Test IDs

- `user-contact-groups-about-page`
- `user-contact-groups-about-tabs`
- `user-contact-groups-about-groups-table`
- `user-contact-groups-about-group-{group_nsid}`
- `user-contact-groups-about-group-toggle-{group_nsid}`
- `user-contact-groups-about-about`
- `user-contact-groups-about-facet-table`
- `user-contact-groups-about-tombstones`
- `user-contact-groups-about-loading`
- `user-contact-groups-about-empty`
- `user-contact-groups-about-error`
- `user-contact-groups-about-partial`
- `user-contact-groups-about-forbidden`
- `user-contact-groups-about-retry`
- `user-contact-groups-about-live-status`

## Out of scope

Joining/leaving groups, browsing group pool photos, changing contact depth,
crawl scheduling controls, sharing controls, contact search, data export and
implementing the requested OpenAPI operations. Build work must first add the
contract requests above to OpenAPI.

## Data

See Data and API above.

## Interactions

See Actions above.

## Test hooks

E2E tests use the stable hooks listed in Test IDs. The page root is
`data-testid="user-contact-groups-about-page"`; the group switch is
`data-testid="user-contact-groups-about-group-toggle-{group_nsid}"`.

### Ready

See the Ready state above.

### Empty

See the Empty state above.

### Error

See the Error state above.
