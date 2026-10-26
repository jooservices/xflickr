# Home

View id `user-home`, role USER, light gallery theme (dark optional). This is
the signed-in landing page for finding photos and checking crawl progress.
Baseline: `vHome()` in the [approved POC](../poc/xflickr-poc.html). Follow the
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), and [i18n](../i18n.md).
Mockup: [user-home.html](../mockups/user-home.html). Build ticket: JOOXF-169.
Parent: JOOXF-64.

## Purpose

Give a signed-in user a photo-led overview of their Flickr circle, searchable
photo records, and current crawl activity. The first visit explains how to
start crawling and links to the setup wizard.

## Route and access

- Route `/`; document title `Home · XFlickr`; user shell Home item active.
- USER role only. A missing or expired session follows the shell's sign-in
  handling; an administrator is redirected to `/admin` by the shell.
- Default appearance is USER light gallery. Respect the saved theme or system
  preference and support USER dark gallery. Set `data-role="user"`.
- Greeting uses the current local hour: morning before 12, afternoon before 18,
  evening otherwise; show the display name's first name.

## Layout

Use the USER shell and its existing navigation. At 1440 px, content fills the
available width with the standard shell gutters. At >= 1100 px the hero is a
two-column row (copy/search at 1.1fr, decorative mosaic at 1fr); below it place
the four-cell stat strip. Main content is a 2:1 grid: the left column holds
"New from your contacts" and the right column stacks Starred contacts, API
quota, and Crawl activity. Cards use existing `card`, `button`, `chip`,
`avatar`, `pill`, `bar`, `list-item`, `photo-grid`, `skeleton`, `banner`, and
`empty-state` patterns; no new component is introduced.

Hero copy has a greeting, one headline, one supporting sentence, a large natural
language search field, a Search button, and four example query chips (two
English and two Vietnamese). The hero mosaic is decorative, uses public photo
thumbnails already present in the user's library, and has no click action.
Below 1100 px, collapse the hero and main grid to one column. At 390 px, stack
the stats two by two, keep the search button visible below the input if needed,
and allow query chips to scroll horizontally. No horizontal page scrolling.

The stats show Contacts (with direct and discovered counts), Photo records,
Sizes ready (count and total), and Captioned (count and total). The POC's
"Calls left this hour" stat is not a substitute for these four requested
measures; quota is shown in the dedicated mini bars.

"New from your contacts" shows up to 12 newest photo records as a responsive
photo grid and a View library link. Starred contacts show up to 5 contacts,
each with avatar, name, photo count, sizes completion, and crawl status; an
All contacts link opens `/contacts`. API quota has one compact usage bar per
valid key, calls used/limit, status text, and reset countdown; Details opens
`/keys`. Crawl activity shows the latest five user-scoped crawl, download, or
quota events with readable severity text and local time; View crawl opens
`/crawl`.

## Data and API

Requests start in parallel. Existing endpoints are used as specified; no
endpoint is called before it exists in the OpenAPI contract and backend.

| Element | Source | Fields used |
| --- | --- | --- |
| Greeting | `GET /me` (exists) | `email`, `locale`; derive the first name from `email` before `@` (the current User schema has no display name) |
| Connected accounts / first-run check | `GET /flickr-accounts` (exists) | array items `id`, `status`, `username` |
| Crawl progress | `GET /me/crawl` (exists) | `depths[]`, `facets[]`, `lanes[]` from `CrawlStatus`; use available totals for direct/discovered contact counts |
| Queue activity fallback | `GET /me/crawl/queue` (exists) | `lanes[]`: `lane`, `backlog_calls`, `effective_calls_per_hour`, `eta_hours` |
| Key list | `GET /keys` (exists) | array items `id`, `label`, `key_public`, `per_hour_limit`, `status` |
| Per-key quota | `GET /keys/{id}/quota` (exists) | `key_id`, `used`, `limit`, `reserve`, `reset_at`, `state`, `lanes` |
| Live activity | authenticated `GET /events` (exists) | `crawl.progress`, `download.progress`, `download.ready`; resume with `Last-Event-ID` |
| Home snapshot contract request | `GET /me/home` (**contract request**) | `contacts.total`, `contacts.direct`, `contacts.discovered`; `photos.total`, `photos.sizes_ready`, `photos.captioned`; `new_photos[]` (`id`, `title`, `thumbnail_url`, `owner_nsid`, `owner_name`, `taken_at`, `captioned`); `starred_contacts[]` (`nsid`, `name`, `icon_url`, `photos_count`, `sizes_ready`, `sizes_total`, `crawl_state`: `idle`, `crawling`, `waiting`, or `attention`); `activity[]` (`id`, `kind`, `message_key`, `occurred_at`, `severity`) |
| Search submission | `/search?q={encoded query}` (view route; no API call from Home) | Navigate with the entered query; search view owns its API contract |

`GET /me/home` returns one user-scoped snapshot, omits private Flickr content,
limits `new_photos` to 12, `starred_contacts` to 5, and `activity` to 5, and
returns counts as non-negative integers. `thumbnail_url` must be an authorized
thumbnail URL. The response has `generated_at` for staleness detection. The
Home build depends on this requested contract; until it exists, render only
sections backed by existing endpoints and do not fabricate counts or records.
If the list API is temporarily unavailable but a previous snapshot exists,
retain it as stale content. No endpoint or OpenAPI change is made in this
design ticket.

## Data

The response fields and existing API sources are listed in [Data and API](#data-and-api).

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Search | Submit query or choose example chip | Trim query; ignore empty input; navigate to `/search?q={URL-encoded query}`. Chips populate and submit their EN or VI example. |
| Open new photo | Activate photo card | Navigate to `/library?photo={id}`; library view owns photo details. |
| View library | Section link | Navigate to `/library`. |
| Open contact | Activate starred contact | Navigate to `/contacts/{nsid}`. |
| All contacts | Section link | Navigate to `/contacts`. |
| Quota details | Details link | Navigate to `/keys`. |
| Crawl details | View crawl link | Navigate to `/crawl`. |
| First-run setup | Empty-state CTA | Navigate to `/onboarding`. |
| Retry | Error retry button | Refetch the failed request(s); preserve successfully loaded sections. |

## Interactions

Action triggers and outcomes are specified in [Actions](#actions).

## States

Each section loads independently. The page may show successful sections while
another section is loading or failed. Loading never blocks the navigation shell.

### Normal

Render all available sections, counts, photo thumbnails, quota bars, and the
activity feed. Missing optional thumbnails use the design-system neutral photo
placeholder. A zero quota key still shows its reset time and a full bar.

### Ready

Ready is the normal state described above: all requested sections have loaded
and the user can search, open records, and follow the section links.

### Loading

On initial load, show content-shaped skeletons for hero counts, the four stats,
12 photo tiles, five starred contacts, key quota rows, and five activity rows.
Keep the shell and hero headline visible. Use the shared skeleton shimmer, which
stops under reduced motion. A background refresh keeps existing values visible
and marks the relevant section busy without replacing it with skeletons.

### Empty

If no Flickr account is connected and there are no crawled records, replace the
photo-led content with the first-run empty state titled "Nothing crawled yet".
Explain that connecting a Flickr account starts contact discovery, show the
"Open setup wizard" primary action to `/onboarding`, and retain the greeting.
If an account is connected but the crawl has not returned records yet, keep the
hero and stats at zero and show "Your photos are on their way" in the new-photo
section. Empty starred contacts and activity sections show one-line empty copy;
quota with no keys links to `/keys` to add a key.

### Error

If all Home data requests fail, show an alert titled "Home could not load",
explain that the data is safe, and provide "Try again". If one section fails,
keep other sections usable and show an inline section error with its own retry.
A 401 is handled by the shell. Do not expose server `message` text.

### Partial and stale

If only some requests fail, retain successful sections and identify failed
sections with an inline warning and retry. When `generated_at` is older than
60 seconds while SSE is disconnected and two polls have failed, show a stale
banner with the last successful update time; keep values readable and label
them as outdated. Unknown quota reset time displays `--:--`; do not guess.

### Forbidden

A 403 for the Home snapshot replaces the page content with a lock empty state:
"You cannot view Home", a short access explanation, and a Back to sign-in link
to `/login`. Do not show retry. A 403 `csrf_rejected` does not apply because
Home actions are navigation or safe reads.

## Live updates

Home is live through the shell's single authenticated SSE connection at
`GET /events`; do not open a second stream. Apply `crawl.progress` to contact,
photo, size, caption counts and crawl activity; apply `download.progress` and
`download.ready` to activity; refresh quota data when a quota state/reset event
arrives. Preserve `Last-Event-ID` on reconnect. Refresh visible key quota
snapshots at most once per 30 seconds while the stream is connected because
crawl calls consume quota. If SSE is unavailable or the shell Live toggle is
off, poll `GET /me/home`, `/me/crawl`, and each visible key's quota every 30
seconds with 30, 60, then 120 second backoff. Pause polling in a hidden tab and
refresh once when visible. Update reset countdown locally every second; pause
it when the tab is hidden. A new activity row is announced once in a polite
live region; changing counts are not announced repeatedly.

## Responsive

| Width | Behaviour |
| --- | --- |
| >= 1100 | Two-column hero, four stats in one strip, 2:1 content grid |
| 721–1099 | Stacked hero, stats in two columns, content grid stacked |
| <= 720 | Two-column stats; stacked cards and hero; horizontally scrollable example chips |
| 390 | Search controls wrap without clipping; photos use two columns; card actions remain >= 44 px |

At 320 px and 200% zoom, content reflows without page-level horizontal scroll.
English and Vietnamese text can grow at least 30%; headings wrap and controls
grow rather than clip.

## Accessibility

- One `h1`; each content section has a visible `h2`; cards use semantic lists.
- Search has a persistent accessible name and Enter submits. Example chips are
  buttons with the query as their name; form validation is announced.
- Photo links have an accessible name including photo title and owner. Hero
  mosaic thumbnails are decorative (`alt=""`). Contact links name the person.
- Quota bars use `role="progressbar"`, `aria-valuenow`, `aria-valuemax`, and
  localized `aria-valuetext` with calls used and limit. Pair severity/status
  color with text or an icon.
- Loading uses `aria-busy`; errors use `role="alert"`; stale state uses
  `role="status"`; the activity announcement region is polite and atomic.
- Focus order follows the visual order. Focus indicator is visible at 2px and
  contrast meets the design-system requirement in both themes. Targets are at
  least 40 px and 44 px on mobile. Respect reduced motion.

## i18n

Keys use `user.home.<element>`; EN is the source and every key has a VI value.
Dates, numbers, relative time, quota reset countdowns, and greeting hour use
the active locale and user time zone. Do not truncate Vietnamese labels.

| Key | EN | VI |
| --- | --- | --- |
| `user.home.title` | Home | Trang chủ |
| `user.home.greeting.{morning,afternoon,evening}` | Good morning · Good afternoon · Good evening | Chào buổi sáng · Chào buổi chiều · Chào buổi tối |
| `user.home.hero.title` | Your Flickr circle, searchable down to every photo. | Vòng kết nối Flickr của bạn, có thể tìm đến từng bức ảnh. |
| `user.home.hero.text` | XFlickr keeps crawling your contacts in the background and lets you find a photo by describing it. | XFlickr tiếp tục thu thập liên hệ trong nền để bạn tìm ảnh bằng cách mô tả. |
| `user.home.search.label` | Search photos | Tìm ảnh |
| `user.home.search.placeholder` | Describe a photo, in English or Vietnamese | Mô tả ảnh bằng tiếng Anh hoặc tiếng Việt |
| `user.home.search.submit` | Search | Tìm kiếm |
| `user.home.search.example.{park,sea}` | a photo with trees, a person and a dog · sunset over the sea | ảnh có cây, người và chó · hoàng hôn trên biển |
| `user.home.search.example.{mountain,city}` | snowy mountains · a city at night | núi phủ tuyết · thành phố về đêm |
| `user.home.stats.{contacts,direct,discovered}` | Contacts · {count} direct · {count} discovered | Liên hệ · {count} trực tiếp · {count} được khám phá |
| `user.home.stats.photos` | Photo records | Bản ghi ảnh |
| `user.home.stats.sizes` | Sizes ready | Đã sẵn kích thước |
| `user.home.stats.captioned` | Captioned | Đã tạo chú thích |
| `user.home.photos.title` | New from your contacts | Ảnh mới từ liên hệ |
| `user.home.photos.all` | View library | Xem thư viện |
| `user.home.contacts.title` | Starred contacts | Liên hệ đã gắn sao |
| `user.home.contacts.all` | All contacts | Tất cả liên hệ |
| `user.home.quota.title` | API quota | Hạn mức API |
| `user.home.quota.details` | Details | Chi tiết |
| `user.home.quota.usage` | {used} of {limit} calls | {used} trên {limit} lượt gọi |
| `user.home.quota.reset` | Resets in {time} | Đặt lại sau {time} |
| `user.home.quota.state.{ok,cooling,exhausted}` | Available · Cooling down · Exhausted | Khả dụng · Đang chờ · Đã hết hạn mức |
| `user.home.activity.title` | Crawl activity | Hoạt động thu thập |
| `user.home.activity.all` | View crawl | Xem tiến độ thu thập |
| `user.home.activity.kind.{crawl,download,quota}` | Crawl · Download · Quota | Thu thập · Tải xuống · Hạn mức |
| `user.home.activity.severity.{info,warning,error}` | Info · Warning · Error | Thông tin · Cảnh báo · Lỗi |
| `user.home.contacts.state.{idle,crawling,waiting,attention}` | Ready · Crawling · Waiting · Needs attention | Sẵn sàng · Đang thu thập · Đang chờ · Cần chú ý |
| `user.home.empty.title` | Nothing crawled yet | Chưa có dữ liệu thu thập |
| `user.home.empty.text` | Connect a Flickr account to start discovering contacts and photos. | Kết nối tài khoản Flickr để bắt đầu khám phá liên hệ và ảnh. |
| `user.home.empty.cta` | Open setup wizard | Mở trình hướng dẫn thiết lập |
| `user.home.empty.photos` | Your photos are on their way. | Ảnh của bạn đang được tải về. |
| `user.home.empty.contacts` | No starred contacts yet. | Chưa có liên hệ nào được gắn sao. |
| `user.home.empty.activity` | No recent activity. | Chưa có hoạt động gần đây. |
| `user.home.empty.quota` | Add an API key to see quota. | Thêm khóa API để xem hạn mức. |
| `user.home.error.title` | Home could not load | Không tải được Trang chủ |
| `user.home.error.text` | Check your connection and try again. Your data is safe. | Kiểm tra kết nối rồi thử lại. Dữ liệu của bạn vẫn an toàn. |
| `user.home.error.section` | This section could not load. | Không tải được mục này. |
| `user.home.error.retry` | Try again | Thử lại |
| `user.home.stale` | Data is out of date. Last updated {time}. | Dữ liệu chưa mới. Cập nhật lần cuối lúc {time}. |
| `user.home.forbidden.{title,text,cta}` | You cannot view Home · Your account does not have access. · Back to sign in | Bạn không thể xem Trang chủ · Tài khoản của bạn không có quyền truy cập. · Quay lại đăng nhập |
| `user.home.status.{loading,ready,partial}` | Loading Home · Updated · Some sections could not update | Đang tải Trang chủ · Đã cập nhật · Một số mục chưa thể cập nhật |

## Test IDs

Each value is a `data-testid` selector. IDs with `{id}` have stable record ids.

- Page and hero: `user-home-page`, `user-home-title`, `user-home-greeting`,
  `user-home-search`, `user-home-search-submit`,
  `user-home-search-example-park`, `user-home-search-example-sea`,
  `user-home-search-example-mountain`, `user-home-search-example-city`,
  `user-home-mosaic`.
- Stats: `user-home-stats`, `user-home-stat-contacts`,
  `user-home-stat-direct`, `user-home-stat-discovered`,
  `user-home-stat-photos`, `user-home-stat-sizes`,
  `user-home-stat-captioned`.
- New photos: `user-home-photos`, `user-home-photo-{id}`,
  `user-home-photos-all`.
- Starred contacts: `user-home-starred`, `user-home-contact-{nsid}`,
  `user-home-contacts-all`.
- Quota: `user-home-quota`, `user-home-quota-key-{id}`,
  `user-home-quota-bar-{id}`, `user-home-quota-reset-{id}`,
  `user-home-quota-details`.
- Activity: `user-home-activity`, `user-home-activity-item-{id}`,
  `user-home-activity-all`, `user-home-live-region`.
- States: `user-home-skeleton`, `user-home-empty`, `user-home-empty-cta`,
  `user-home-error`, `user-home-error-retry`, `user-home-stale`,
  `user-home-section-error-{section}`, `user-home-forbidden`.

## Test hooks

The complete E2E selector list is in [Test IDs](#test-ids). Select controls by
accessible role first and these stable IDs when text varies by locale.

## Out of scope

- Search results and search API; photo detail and library filtering; contact
  detail; key editing; crawl controls; setup wizard internals; notification
  center; changes to the shared shell or SSE event contract.
- OpenAPI and backend changes. The `GET /me/home` contract request is recorded
  above for its owning API ticket; this design ticket changes documentation
  only.
