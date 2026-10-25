# Crawl

View id `user-crawl`, role USER, light gallery theme (dark optional), route
`/crawl`. Baseline: `vCrawl()` and `jobDrawer()` in the approved
[POC](../poc/xflickr-poc.html) (Crawl tab, USER role). Shared rules:
[design system](../design-system.md), [components](../components.md),
[patterns](../patterns.md), [accessibility](../a11y.md), [i18n](../i18n.md).
Mockup: [user-crawl.html](../mockups/user-crawl.html). Build ticket: JOOXF-175.
The view renders inside the [user shell](user-shell.md).

## Purpose

Let a person steer the crawler and see what it does: how deep it follows
contacts, how much work waits in each lane and when it ends, which contacts
come first, and why it sometimes stands still (quota wait, exhausted key, no
key, paused). A job drawer explains any single job.

## Route and access

- `/crawl`, role USER (ADMIN may open it and sees only their own crawl; the
  fleet view is `admin-pipeline`). Unauthenticated: the shell sends the user to
  `/login?reason=expired&return_to=/crawl`.
- Document title `Crawl · XFlickr` (`user.crawl.title`). Focus moves to the
  `h1` after navigation.
- The server limits data to the signed-in user; the UI never sends a user id.
- Deep links: `/crawl?job=<id>` opens the job drawer (used by notifications);
  `/crawl?depth=<n>` scrolls to and highlights that depth column. Both queries
  are removed when the drawer closes or the highlight ends.

## Layout

Inside the shell main column (max width 1200 px, 24 px padding, 16 px on
mobile), top to bottom.

1. Page head: `h1`, lead `user.crawl.lead`, and at the right the secondary
   button `user.crawl.pause-all` / `user.crawl.resume-all` (full width under the
   lead on mobile).
2. Status banner slot: zero or one `banner` (see Ready, quota states).
3. Three `card`s (`grid g3`, one column below 900 px):
   - **Depth**: help text, a range input from 1 to the cap, the labels
     `user.crawl.depth.min` and the current value, and `user.crawl.depth.cap`.
     The button `user.crawl.depth.save` shows while the value differs from the
     saved one.
   - **Lanes** (live): **Person lane** and **Sizes lane**. Each shows backlog
     calls (`num`), a `progress-bar` of done/(done+backlog) for the cycle,
     `Queue position #n` and `ETA` (`~3 h 20 min`, `< 1 min`; en dash when
     nothing waits or `eta_hours` is null), then the muted line
     `user.crawl.lanes.hint`.
   - **Recrawl**: as in the POC: interval text and a `kv` list (cycle
     progress, calls saved this week).
4. **Per depth** panel: one column per depth from 0 to the current depth
   (0 = your Flickr accounts), scrolling horizontally on mobile. A column has
   its title, the contact count (`num`), a stacked `progress-bar` (done,
   crawling, waiting, error) with a text legend and "{done} of {total} done".
   Columns deeper than the setting but holding data are muted with the badge
   `user.crawl.depth.beyond` (kept, no new discovery). Clicking a column
   filters the contacts table.
5. **Contacts in your crawl** panel: filter chips, a table (Contact, Depth,
   Status, Photos, Sizes, Last check, actions) with "Load more" (50 rows).
   Order: starred, selected, depth ascending, name; rows never re-sort when a
   status changes. On mobile each row is a card with actions below.
6. **Active jobs** panel: the 10 latest non-done jobs (Job, Contact, Facet,
   State, Attempt) as a compact table; a row opens the drawer; the link
   `user.crawl.jobs.all` loads 50 more.
7. **Job drawer** (right side 480 px, bottom sheet on mobile): job id and
   state `pill`; `kv` list: Contact, Facet, Page, Lane, State, Attempt "{n} of
   {max}", Retries, Key, Priority, API calls, Records, Created, Started,
   Finished, Next retry; the red `Last error` row; an Attempts table (number,
   time, result, duration). Footer: `Retry now` (only for `retry` and
   `failed`) and a Close icon button in the header.

Components used: `card`, `pill`, `badge`, `progress-bar`, `kv` list, `table`,
`filter chip`, `banner`, `empty-block`, `skeleton`, `drawer`, `button`,
`icon-btn`, `toast`, range slider as in the POC. No new component. Deviation
from the POC, decided here: "Star" is added to the row actions and the
drawer's "Cancel job" is dropped (see Out of scope).

## Data

Five sources feed the page; each section renders when its own request
answers. Fields are in [Data and API](#data-and-api).

## Data and API

| Element | Source | Fields used |
| --- | --- | --- |
| Lanes, ETA | `GET /me/crawl/queue` (exists) | `lanes[]` of `lane` (`person`, `sizes`), `backlog_calls`, `effective_calls_per_hour`, `eta_hours` (null = no capacity) |
| Lane position and run state | `GET /me/crawl/queue` **contract request** | adds to the response `state` (`running`, `waiting_quota`, `key_exhausted`, `no_key`, `paused`), `resets_at` (earliest window reset, null unless waiting), `server_time`, `exhausted_keys[]` of `{id, label}`; adds per lane `position` (1-based place of the user's next item in that lane's global dispatch order, null when nothing waits), `done_calls` (completed this cycle) |
| Settings | `GET /me/crawl/settings` **contract request** | `depth` (saved), `default_depth`, `max_depth` (effective cap = min(plan `max_depth_cap`, administrator maximum)), `paused` bool |
| Change settings | `PUT /me/crawl/settings` **contract request** body `{depth?, paused?}` | 200 settings; 422 `depth_above_cap` (`details.max_depth`); changing depth never deletes data |
| Per depth | `GET /me/crawl/depths` **contract request** | `items[]` of `depth`, `contacts_total`, `done`, `crawling`, `waiting`, `error`, `beyond_setting` bool |
| Contacts | `GET /me/crawl/interests?filter=&depth=&cursor=&limit=` **contract request** (reads `crawl_interests`) | `items[]` of `person_id`, `nsid`, `name`, `avatar_url`, `depth`, `starred`, `selected_until`, `paused`, `status` (`done`, `crawling`, `queued`, `paused`, `error`), `photos_crawled`, `photos_total`, `sizes_percent`, `last_check_at`; `next_cursor` |
| Star, select, pause contact | `PUT /me/crawl/interests/{person_id}` **contract request** body `{starred?, selected?, paused?}` | 200 item; `selected: true` sets `selected_until` to now + 24 h (server decides) and moves the contact to the front of both lanes |
| Recrawl card | `GET /me/crawl/summary` **contract request** | `recrawl_interval_hours`, `contacts_done`, `contacts_total`, `calls_saved_week` |
| Jobs | `GET /me/crawl/jobs?state=&limit=&cursor=` **contract request** | `items[]` of `id`, `person_id`, `contact_name`, `facet`, `lane`, `state`, `attempt`, `max_attempts`, `page`, `pages`, `created_at` |
| Job detail | `GET /me/crawl/jobs/{id}` **contract request** | the list fields plus `priority` (`selected`, `starred`, `depth`, `normal`), `key_label`, `calls`, `records`, `error` `{code, message}` (code localised, message English and shown only inside a mono block), `started_at`, `finished_at`, `next_retry_at`, `attempts[]` of `n`, `at`, `result`, `duration_ms` |
| Retry job | `POST /me/crawl/jobs/{id}/retry` **contract request** | 202 job in `pending`; 409 `job_not_retryable` |
| Live updates | `GET /api/v1/events/stream` (SSE, shell connection) | channel `crawl.user`: `crawl.queue` (the queue response above), `crawl.progress` `{person_id, status, photos_crawled, sizes_percent}`, `crawl.job` (a job summary), `crawl.settings` `{depth, paused}` |
| Error body | shared `Error` | `code`, `message` (English, never shown), `details` |

`facet` values: `photos`, `favorites`, `albums`, `album_photos`, `contacts`,
`galleries`, `groups`, `profile`, `sizes`. The `lane` of a job is `sizes` for
`sizes` and `person` for the rest.

Decisions made here (the POC does not show them):

- Banner precedence: `no_key`, `paused`, `waiting_quota` (every valid key is at
  its cap, or in reserve so only user-started work may run), `key_exhausted`
  (one key at its cap while another still crawls), then `running` (no banner).
  The server returns the state; the client never derives it.
- Slider range is 1 to `max_depth`. Lowering depth stops discovering deeper
  contacts but keeps stored data. A warning line (`user.crawl.depth.warn`)
  shows when the value is more than 2 above the default.
- "Selected" lasts 24 h (`selected_until`); "starred" lasts until unstarred.
  Priority order is selected, starred, lower depth, as in the scheduler.
- Pause all pauses scheduled calls only; downloads and user-started actions
  still run. Pausing a contact stops that contact's jobs.
- Queue position #1 means the user's next item is next to dispatch in that
  lane. Countdown is `resets_at - (server_time + elapsed since the
  response)`, never the browser clock alone.
- Contract requests are only requested here; the build tickets change
  OpenAPI. Unsafe calls need `X-CSRF-Token`.

## Actions

| Action | Trigger | Effect |
| --- | --- | --- |
| Change depth | slider, Save | Slider moves locally; Save calls `PUT /me/crawl/settings` `{depth}`; toast `user.crawl.toast.depth`; the per-depth row refetches. 422 resets the slider to the saved value and shows `user.crawl.error.depth-cap` |
| Pause all / Resume all | page head button | `PUT /me/crawl/settings` `{paused}`; button shows a spinner meanwhile; banner and lanes follow; toast `user.crawl.toast.paused` / `resumed` |
| Star, Prioritize, Pause or Resume contact | row buttons | `PUT` interest `{starred}`, `{selected: true}` (badge `user.crawl.selected`, toast `user.crawl.toast.selected`) or `{paused}`; star is optimistic and rolled back with a toast on failure |
| Filter, Load more | chips, depth column, button | Filter refetches the list with the cursor reset and mirrors `?filter=`; Load more appends `next_cursor`, focus stays on the button |
| Open job | row click or Enter | Opens the drawer; `GET` detail; focus moves into the drawer, `?job=` set |
| Retry job | drawer button | `POST` retry with a spinner; on 202 the state becomes `pending` and toast `user.crawl.toast.retry` shows |
| Banner action | banner button | `no_key`: link to `/keys?add=1`; `key_exhausted`: link to `/keys`; `paused`: Resume all; `waiting_quota`: link to `/keys` |
| Retry load | error or stale banner | Refetches the failed requests only |

## Interactions

- Slider: arrows change by 1, Home and End jump to the ends; Save is disabled
  only while sending. A double click on star, pause or Retry is ignored while
  a request is in flight.
- Filtering and paging keep the scroll position; a filter change moves focus
  to the table caption. Esc or Close closes the drawer and returns focus to
  the opening row; the page behind does not scroll; the deep link works cold.
- Tab order: head button, banner action, slider, Save, depth columns, chips,
  row actions, Load more, jobs.

## States

### Ready

All sections loaded, live updates on. Normal state in the mockup.

Quota states (one banner, from `state`; the Lanes card stays and shows the
reason in place of the ETA):

- **Waiting for quota reset** (`waiting_quota`): warn `banner`
  `user.crawl.banner.waiting` with a mm:ss countdown (`{time}`) to `resets_at`,
  action `user.crawl.banner.keys`. At zero the view fetches the queue at once
  and again after 3 s; the banner disappears by itself.
- **Key exhausted** (`key_exhausted`): info `banner`
  `user.crawl.banner.exhausted` naming the key labels; crawling continues on
  the other keys; action `user.crawl.banner.keys`.
- **No key** (`no_key`): bad `banner` `user.crawl.banner.no-key`, action
  `user.crawl.banner.add-key` (to `/keys?add=1`). Lanes show backlog and ETA
  `—`; contacts keep their stored status.
- **Paused by user** (`paused`): info `banner` `user.crawl.banner.paused`,
  action Resume all (same as the head button). Lanes show ETA `—`; the Pause
  all button is replaced by Resume all.

### Loading

First load: after 150 ms show the `skeleton`: the page head stays, then three
card blocks, depth column blocks and six table row bars; `aria-busy` on the
body and one `role=status` line `user.crawl.loading`. Sections appear as they
answer. Timeout 10 s then Error. Refresh keeps data (no skeleton).

### Empty

`GET /me/crawl/interests` returned no items and no filter is active:
`empty-block` with the activity icon, title `user.crawl.empty.title`, text
`user.crawl.empty.text` and one call to action. With no valid key the CTA is
`user.crawl.empty.cta-key` (to `/keys?add=1`); with a key but no connected
Flickr account it is `user.crawl.empty.cta-account` (to `/accounts`); the
status endpoint decides from `state`. The Depth card stays; Lanes, Per depth
and Active jobs are hidden. A filter with no match shows
`user.crawl.empty.filtered` with `user.crawl.empty.clear` instead.

### Error

- Every request failed (network, 5xx, timeout): `empty-block` with the alert
  icon, `user.crawl.error.title`, `user.crawl.error.text` and button
  `user.common.retry` (`user-crawl-error-retry`); focus moves to the block.
- One section failed: inline `user.crawl.error.section` with its own retry;
  the rest stays.
- Save depth, settings or a contact action failed: toast (bad)
  `user.crawl.error.action-failed`, controls and row rolled back. 422
  `depth_above_cap`: `user.crawl.error.depth-cap`. A 404 removes the row.
- Drawer detail failed: `user.crawl.error.job` with retry; 404 shows
  `user.crawl.job.gone`. Retry job 409: toast `user.crawl.error.not-retryable`
  and the drawer refetches.

### Partial and stale

- Queue failed but lists loaded: Lanes show en dashes and a warn `banner`
  `user.crawl.stale.queue` with `user.common.retry` (`user-crawl-stale-retry`).
- Data older than 30 s while live is off or the stream is down (two missed
  polls): the same banner with `{time}` of the last update; numbers stay
  readable and the countdown shows `--:--` instead of guessing.

### Forbidden

403 on the first request: the page body is replaced by an `empty-block` with a
lock icon, `user.crawl.forbidden.title`, `.text` and a link
`user.crawl.forbidden.cta` to `/`; no retry. 403 `csrf_rejected` on an action
refreshes the token once and repeats; a second 403 shows
`user.crawl.error.action-failed`.

## Live updates

The view is live. It uses the shell's single SSE connection (channel
`crawl.user`) and updates in place: lanes, banner, per-depth counts, contact
status and progress, the job list and the open drawer (state, attempt,
next-retry countdown). A changed number gets a 600 ms accent background
(`fresh`, off with reduced motion). Fallback when SSE is down or Live is off:
poll `GET /me/crawl/queue` every 15 s (backoff 15, 30, 60 s), jobs and the open
drawer every 10 s, depths and visible contacts every 30 s; pause when the tab
is hidden and fetch once on show. Countdowns use a local 1 s timer. Events are
buffered while Live is paused and the view refetches on resume. A change of run
state is announced once through the polite live region
(`user.crawl.live.state-changed`), never every job.

## Responsive

| Width | Behaviour |
| --- | --- |
| >= 1101 | Three cards in a row; table with all columns |
| 900 to 1100 | Three cards; Recrawl wraps under Lanes when too narrow |
| < 900 | Cards stack; per-depth row scrolls horizontally; head button full width |
| 390 | Contacts as cards with 44 px actions; jobs as two-line rows; drawer as bottom sheet with sticky footer; chips scroll; no horizontal scroll down to 320 px or 200 % zoom |

## Accessibility

- One `h1`; sections are labelled with `h2`/`h3`. The table has a caption and
  `scope` headers; chips are buttons with `aria-pressed`.
- Progress bars are `role=progressbar` with `aria-valuenow`, `aria-valuemax`
  and `aria-valuetext` ("{done} of {total} done"); the per-depth stack also
  has a text legend, never colour only.
- The slider has a visible label, `aria-valuetext` "Depth {n}" and the cap in
  its description.
- Banners are `role=status` (waiting, exhausted, paused) or `role=alert` (no
  key, errors). Countdowns are not live regions.
- Drawer: `role=dialog`, `aria-modal`, labelled by its title, focus trapped
  and restored. Row buttons name the contact (`aria-label`).
- Contrast >= 4.5:1 in light and dark; reduced motion stops shimmer, spinner
  and `fresh`. Targets >= 40 px (44 px on mobile).

## i18n

Keys follow `user.crawl.<element>` (i18n.md). EN is the source; every key has
a VI text. Numbers, times and ETAs use the active locale. Text grows by 30 %:
buttons wrap, labels never truncate silently.

| Key | EN | VI |
| --- | --- | --- |
| `user.crawl.title` | Crawl | Thu thập |
| `user.crawl.lead` | XFlickr crawls in the background with your API keys. Each contact is crawled fully, then the cycle starts over with cheap change checks that fetch only new photos. | XFlickr thu thập ngầm bằng khóa API của bạn. Mỗi liên hệ được thu thập đầy đủ, sau đó chu kỳ lặp lại với bước kiểm tra thay đổi nhẹ, chỉ lấy ảnh mới. |
| `user.crawl.{pause-all,resume-all}` | Pause crawl · Resume crawl | Tạm dừng thu thập · Tiếp tục thu thập |
| `user.crawl.depth.title` | Depth | Độ sâu |
| `user.crawl.depth.help` | How far to follow favorites. | Mức theo dõi các ảnh yêu thích. |
| `user.crawl.depth.value` | Depth {n} | Độ sâu {n} |
| `user.crawl.depth.min` | 1 · your contacts | 1 · liên hệ của bạn |
| `user.crawl.depth.cap` | Default {default}, your maximum {max}. | Mặc định {default}, tối đa của bạn {max}. |
| `user.crawl.depth.warn` | Each level can multiply contacts by 10 or more. | Mỗi mức có thể nhân số liên hệ lên 10 lần trở lên. |
| `user.crawl.depth.save` | Save depth | Lưu độ sâu |
| `user.crawl.depth.beyond` | Above setting | Vượt cài đặt |
| `user.crawl.lanes.title` | Your lanes | Các luồng của bạn |
| `user.crawl.lane.{person,sizes}` | Person lane · Sizes lane | Luồng người dùng · Luồng kích thước |
| `user.crawl.lane.backlog` | {count} calls waiting | {count} lượt gọi đang chờ |
| `user.crawl.lane.position` | Queue position #{n} | Vị trí hàng đợi #{n} |
| `user.crawl.lane.eta` | ETA {time} | Dự kiến {time} |
| `user.crawl.lanes.hint` | The contact you chose comes first, then starred, then by depth. | Liên hệ bạn chọn được làm trước, rồi đến liên hệ gắn sao, rồi theo độ sâu. |
| `user.crawl.recrawl.title` | Recrawl | Thu thập lại |
| `user.crawl.recrawl.text` | Change check every {hours} h per contact: 1 to 2 calls, then only new photos. | Kiểm tra thay đổi mỗi {hours} giờ cho mỗi liên hệ: 1 đến 2 lượt gọi, sau đó chỉ lấy ảnh mới. |
| `user.crawl.recrawl.{cycle,saved}` | {done} of {total} contacts done · {count} calls saved this week | {done} trên {total} liên hệ đã xong · Tiết kiệm {count} lượt gọi tuần này |
| `user.crawl.depths.title` | Per depth | Theo độ sâu |
| `user.crawl.depths.own` | Your accounts | Tài khoản của bạn |
| `user.crawl.depths.column` | Depth {n} | Độ sâu {n} |
| `user.crawl.depths.{count,done}` | {count} contacts · {done} of {total} done | {count} liên hệ · {done} trên {total} đã xong |
| `user.crawl.depths.legend` | done · crawling · waiting · error | xong · đang chạy · đang chờ · lỗi |
| `user.crawl.contacts.title` | Contacts in your crawl | Liên hệ trong lượt thu thập |
| `user.crawl.filter.{all,starred,selected,crawling,attention}` | All · Starred · Selected · Crawling · Needs attention | Tất cả · Gắn sao · Đã chọn · Đang thu thập · Cần chú ý |
| `user.crawl.col.contact` | Contact | Liên hệ |
| `user.crawl.col.status` | Status | Trạng thái |
| `user.crawl.col.{photos,sizes,checked}` | Photos · Sizes · Last check | Ảnh · Kích thước · Kiểm tra lần cuối |
| `user.crawl.status.{done,crawling,queued,paused,error}` | Done · Crawling · Queued · Paused · Error | Đã xong · Đang thu thập · Đang chờ · Đã tạm dừng · Lỗi |
| `user.crawl.selected` | Selected | Đã chọn |
| `user.crawl.{star,unstar}` | Star {name} · Remove star from {name} | Gắn sao {name} · Bỏ sao của {name} |
| `user.crawl.prioritize` | Prioritize | Ưu tiên |
| `user.crawl.prioritize.aria` | Prioritize {name} | Ưu tiên {name} |
| `user.crawl.{pause,resume}` | Pause · Resume | Tạm dừng · Tiếp tục |
| `user.crawl.load-more` | Load more | Tải thêm |
| `user.crawl.jobs.title` | Active jobs | Công việc đang hoạt động |
| `user.crawl.jobs.all` | Show more jobs | Xem thêm công việc |
| `user.crawl.facet.{photos,favorites,albums,album_photos,contacts,galleries,groups,profile,sizes}` | Photos · Favorites · Albums · Album photos · Contacts · Galleries · Groups · Profile · Sizes | Ảnh · Yêu thích · Album · Ảnh trong album · Liên hệ · Bộ sưu tập · Nhóm · Hồ sơ · Kích thước |
| `user.crawl.job.state.{pending,running,waiting,retry,failed,done}` | Pending · Running · Waiting for quota · Retrying · Failed · Done | Chờ xử lý · Đang chạy · Chờ hạn mức · Đang thử lại · Thất bại · Đã xong |
| `user.crawl.job.title` | Crawl job | Công việc thu thập |
| `user.crawl.job.id` | Job | Công việc |
| `user.crawl.job.facet` | Facet | Loại dữ liệu |
| `user.crawl.job.page` | Page {page} of {pages} | Trang {page} trên {pages} |
| `user.crawl.job.{lane,state}` | Lane · State | Luồng · Trạng thái |
| `user.crawl.job.attempt` | {n} of {max} | {n} trên {max} |
| `user.crawl.job.retries` | Retries | Số lần thử lại |
| `user.crawl.job.{key,priority}` | Key · Priority | Khóa · Ưu tiên |
| `user.crawl.job.{calls,records}` | API calls · Records | Lượt gọi API · Bản ghi |
| `user.crawl.job.{created,started,finished}` | Created · Started · Finished | Tạo lúc · Bắt đầu · Kết thúc |
| `user.crawl.job.next-retry` | Next retry in {time} | Thử lại sau {time} |
| `user.crawl.job.error` | Last error | Lỗi gần nhất |
| `user.crawl.job.{attempts,attempts-empty}` | Attempts · The first attempt is running. | Các lần thử · Lần thử đầu đang chạy. |
| `user.crawl.job.retry` | Retry now | Thử lại ngay |
| `user.crawl.job.close` | Close | Đóng |
| `user.crawl.job.gone` | This job finished and left the list. | Công việc này đã xong và không còn trong danh sách. |
| `user.crawl.banner.waiting` | Waiting for the quota to reset in {time}. Crawling continues by itself. | Đang chờ đặt lại hạn mức sau {time}. Thu thập sẽ tự tiếp tục. |
| `user.crawl.banner.exhausted` | {keys} reached its hourly cap. Crawling continues on your other keys. | {keys} đã chạm hạn mức theo giờ. Thu thập tiếp tục trên các khóa khác của bạn. |
| `user.crawl.banner.no-key` | You have no API key, so nothing can be crawled. | Bạn chưa có khóa API nên chưa thể thu thập. |
| `user.crawl.banner.paused` | Crawling is paused. Stored photos stay available. | Thu thập đang tạm dừng. Ảnh đã lưu vẫn dùng được. |
| `user.crawl.banner.{keys,add-key}` | Open Keys & quota · Add API key | Mở Khóa & hạn mức · Thêm khóa API |
| `user.crawl.loading` | Loading crawl… | Đang tải thu thập… |
| `user.crawl.empty.title` | Nothing to crawl yet | Chưa có gì để thu thập |
| `user.crawl.empty.text` | Add an API key and connect a Flickr account. Your contacts appear first, then their photos. | Hãy thêm khóa API và kết nối tài khoản Flickr. Liên hệ hiện trước, rồi đến ảnh của họ. |
| `user.crawl.empty.{cta-key,cta-account}` | Add API key · Connect Flickr account | Thêm khóa API · Kết nối tài khoản Flickr |
| `user.crawl.empty.{filtered,clear}` | No contacts match this filter. · Show all | Không có liên hệ nào khớp bộ lọc này. · Hiện tất cả |
| `user.crawl.error.title` | Crawl could not load | Không tải được thu thập |
| `user.crawl.error.text` | Check your connection and try again. Your data is safe. | Hãy kiểm tra kết nối và thử lại. Dữ liệu của bạn vẫn an toàn. |
| `user.crawl.error.action-failed` | That did not work. Nothing changed. | Thao tác không thành công. Chưa có gì thay đổi. |
| `user.crawl.error.depth-cap` | Your maximum depth is {max}. Pick a lower value. | Độ sâu tối đa của bạn là {max}. Hãy chọn giá trị thấp hơn. |
| `user.crawl.error.{job,not-retryable}` | The job could not load. · This job cannot be retried now. | Không tải được công việc. · Hiện không thể thử lại công việc này. |
| `user.crawl.error.section` | This section could not load. | Không tải được phần này. |
| `user.crawl.stale.queue` | Queue numbers are out of date (last update {time}). | Số liệu hàng đợi chưa mới (cập nhật lúc {time}). |
| `user.crawl.forbidden.{title,text}` | You cannot view the crawl · Your account does not have access to this page. Ask the administrator. | Bạn không thể xem thu thập · Tài khoản của bạn không có quyền vào trang này. Hãy hỏi quản trị viên. |
| `user.crawl.forbidden.cta` | Back to Home | Về Trang chủ |
| `user.crawl.toast.depth` | Depth set to {n} | Đã đặt độ sâu {n} |
| `user.crawl.toast.{paused,resumed}` | Crawl paused · Crawl resumed | Đã tạm dừng thu thập · Đã tiếp tục thu thập |
| `user.crawl.toast.selected` | {name} moved to the front | {name} đã được đưa lên đầu |
| `user.crawl.toast.retry` | Job queued again | Công việc đã được xếp lại |
| `user.crawl.live.state-changed` | Crawl is now {state}. | Thu thập hiện ở trạng thái {state}. |
| `user.common.retry` | Try again | Thử lại |

## Test IDs

Each name is a `data-testid` value (`user-crawl-<element>`, patterns.md). A
person id, job id, lane or depth number is the stable suffix. Tests select by
role first and by test id when text depends on language.

- Page: `user-crawl-page`, `user-crawl-title`, `user-crawl-lead`,
  `user-crawl-pause-all`, `user-crawl-skeleton`, `user-crawl-live-region`.
- Banners: `user-crawl-banner-waiting`, `user-crawl-banner-exhausted`,
  `user-crawl-banner-no-key`, `user-crawl-banner-paused`,
  `user-crawl-banner-countdown`, `user-crawl-banner-action`.
- Depth: `user-crawl-depth-card`, `user-crawl-depth-slider`,
  `user-crawl-depth-value`, `user-crawl-depth-cap`, `user-crawl-depth-save`.
- Lanes: `user-crawl-lanes-card`, `user-crawl-lane-person`,
  `user-crawl-lane-sizes`, `user-crawl-lane-position-<lane>`,
  `user-crawl-lane-eta-<lane>`, `user-crawl-lane-backlog-<lane>`,
  `user-crawl-recrawl-card`.
- Per depth: `user-crawl-depths`, `user-crawl-depth-column-<n>`.
- Contacts: `user-crawl-contacts`, `user-crawl-filter-<name>`,
  `user-crawl-contact-<id>`, `user-crawl-contact-status-<id>`,
  `user-crawl-contact-star-<id>`, `user-crawl-contact-prioritize-<id>`,
  `user-crawl-contact-pause-<id>`, `user-crawl-load-more`.
- Jobs: `user-crawl-jobs`, `user-crawl-job-<id>`, `user-crawl-jobs-more`.
- Drawer: `user-crawl-job-drawer`, `user-crawl-job-drawer-id`,
  `user-crawl-job-drawer-state`, `user-crawl-job-drawer-contact`,
  `user-crawl-job-drawer-facet`, `user-crawl-job-drawer-page`,
  `user-crawl-job-drawer-attempt`, `user-crawl-job-drawer-error`,
  `user-crawl-job-drawer-times`, `user-crawl-job-drawer-attempts`,
  `user-crawl-job-drawer-retry`, `user-crawl-job-drawer-close`.
- States: `user-crawl-empty`, `user-crawl-empty-cta`, `user-crawl-error`,
  `user-crawl-error-retry`, `user-crawl-stale-banner`,
  `user-crawl-stale-retry`, `user-crawl-forbidden`,
  `user-crawl-forbidden-cta`.

## Test hooks

The list above is the contract for `web/e2e/user-crawl.spec.ts`. Journeys:
empty state and its call to action; depth Save and the cap error; Pause all
then Resume; the four quota banners with the countdown reaching zero; star,
prioritize and pause a contact; filter chip and depth column; open a job from
the list and by `?job=`, Retry now on a failed job; live `crawl.queue` event;
polling fallback; stale banner; section error retry; 403; EN/VI switch; 390 px
without horizontal scroll.

## Out of scope

- Cancelling jobs, tuning backoff and fleet pipeline state (`admin-pipeline`);
  administrator caps (`admin-defaults`); key management (`user-keys`);
  contact detail (`user-contacts`, `user-contact`); group pool crawling.
- Notification wording, the shell Live toggle (user-shell), OpenAPI edits
  (only requested above) and application code.
