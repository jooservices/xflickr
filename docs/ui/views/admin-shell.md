# Admin shell, rail and navigation

View id `admin-shell`. Mockup: [admin-shell.html](../mockups/admin-shell.html).
Build ticket: JOOXF-79. Baseline: [POC](../poc/xflickr-poc.html) (`topbar()`,
`rail()`, `A_NAV`). Shared rules: [design system](../design-system.md),
[components](../components.md), [patterns](../patterns.md),
[accessibility](../a11y.md), [i18n](../i18n.md).

## Purpose

The shell is the frame of every ADMIN view: a dark control-room top bar, a left
rail with the 14 admin destinations, a cluster health footer, and the content
region where each admin view renders. It answers three questions on every
screen: where am I, is the cluster healthy, and does anything need a person
(open issues). It adds no data screen of its own. Each destination is specified
in its own view file and is out of scope here.

## Route and access

- Layout route for every path under `/admin` (`/admin`, `/admin/nodes`,
  `/admin/nodes/:id`, `/admin/quota`, `/admin/metrics`, `/admin/events`,
  `/admin/issues`, `/admin/pipeline`, `/admin/downloads`, `/admin/sync`,
  `/admin/ai`, `/admin/users`, `/admin/defaults`, `/admin/sharing`,
  `/admin/log`). The `Cluster node` detail route highlights `Cluster nodes`.
- Role: ADMIN only. The guard reads `GET /me` (`role`) before rendering any
  child route.
  - No session (401): redirect to `/login?next=<path>`.
  - Role `user` (decided): render the 403 page from
    [patterns](../patterns.md) (lock icon, title "You do not have access to
    this page", CTA `Go to home` to `/`) inside the public layout, at the same
    URL, with no rail, no admin data fetch and no redirect. The shell never
    leaks node counts, issue counts or route existence to a non-admin.
  - Any admin API answering 403 `forbidden` while the page is open (role
    demoted) switches to the same 403 page and closes the SSE connection.
- The role switch is not a user feature. A user with role `user` never sees
  admin chrome. An `admin` sees a two-button group in the top bar, `User` and
  `Admin` (`aria-pressed`), that navigates between `/` and `/admin`. It is
  absent for role `user`. The POC role switch that fakes roles is not built.
- Theme: ADMIN dark by default, light on request (sun/moon button); stored per
  user and applied through `data-role="admin"` and `data-theme`.

## Layout

Desktop (above 900 px), grid `226px minmax(0, 1fr)` below a sticky 56 px top bar:

```text
+----------------------------------------------------------------+
| ≡  XFlickr   [User|Admin]        ●Live  EN  ☾  🔔2  (avatar)    | top bar
+----------+-----------------------------------------------------+
| OPERATIONS| content region (<main id="main">, view h1 first)   |
|  Overview |                                                     |
|  Cluster nodes                                                  |
|  API quota                                                      |
|  Metrics                                                        |
|  Events & audit                                                 |
|  Issues  [3]                                                    |
| PIPELINES |                                                     |
|  Crawl pipeline / Downloads / Cloud sync / AI index             |
| DATA & PEOPLE                                                   |
|  Users / Defaults / Sharing / API call log                      |
| [cluster health footer]                                         |
+----------+-----------------------------------------------------+
```

Top bar (shared component with the user shell, same order and placement
everywhere): menu button (below 900 px only), brand `XFlickr` (links to
`/admin`), role switch, then right-aligned actions: Live/Paused toggle, EN/VI
toggle, theme toggle, notifications bell with unread count, profile avatar
(links to `/admin/users`). The user-shell search box and section tabs do not
appear in admin.

Rail (`nav`, aria-label "Admin sections"), 226 px, `surface` fill, 1 px `line`
right border, scrolls independently:

| Group | Item | Route | Icon |
| --- | --- | --- | --- |
| Operations | Overview | `/admin` | grid |
| Operations | Cluster nodes | `/admin/nodes` | server |
| Operations | API quota | `/admin/quota` | key |
| Operations | Metrics | `/admin/metrics` | chart |
| Operations | Events & audit | `/admin/events` | list |
| Operations | Issues (badge) | `/admin/issues` | flag |
| Pipelines | Crawl pipeline | `/admin/pipeline` | activity |
| Pipelines | Downloads | `/admin/downloads` | download |
| Pipelines | Cloud sync | `/admin/sync` | cloud |
| Pipelines | AI index | `/admin/ai` | sparkle |
| Data & people | Users | `/admin/users` | users |
| Data & people | Defaults | `/admin/defaults` | sliders |
| Data & people | Sharing | `/admin/sharing` | share |
| Data & people | API call log | `/admin/log` | eye |

- Group label: `eyebrow` style, `faint`. Item: 16 px icon, label, 6 px / 10 px
  padding, radius 6, `muted` text; hover `sunken` and `ink`; current item
  (`aria-current="page"`) `accent-soft` fill, `ink` text, `accent` icon.
- Issues badge: `badge` component with the open issue count, hidden at 0.
- Cluster health footer, pinned to the rail bottom (`margin-top: auto`): bordered
  box with `Cluster` eyebrow, a `pill` (`Healthy` ok, `Degraded` warn,
  `Down` bad), the line `{calls} calls/s · {healthy}/{total} nodes` (mono,
  tabular) and a 60 point `sparkline` of cluster calls per second (series `s1`).
  The whole footer is a link to `/admin/nodes`.
- Components used: button, pill, badge, banner, sparkline, skeleton,
  empty-block (all in [components](../components.md)). The rail, top bar and
  mobile drawer are shell layout in `web/src/app/shell`, not new shared
  components.

## Data

| UI element | Source | Fields |
| --- | --- | --- |
| Role guard, role switch | `GET /me` | `role` (`admin`/`user`), `locale`, `email` |
| Avatar, name | `GET /me` | `email` (initials from the part before `@`) |
| Issues badge | `GET /admin/shell` (request) | `open_issues` |
| Footer pill, counts | `GET /admin/shell` (request) | `cluster.status`, `cluster.nodes_healthy`, `cluster.nodes_total`, `cluster.calls_per_second` |
| Footer sparkline | `GET /admin/shell` (request) | `cluster.calls_history[60]` (one integer per minute, oldest first) |
| Bell count | `GET /admin/shell` (request) | `unread_notifications` |
| Language | `GET /me`, `PATCH /me` (request) | `locale` (`en`/`vi`) |

The current route title, breadcrumb and page content come from each view.

## Data and API

Contract requests (not in `contracts/openapi` today; JOOXF-79 must not invent
other shapes):

1. `GET /admin/shell`, `RequireAdmin`. Response
   `{ "open_issues": int, "unread_notifications": int, "cluster": { "status":
   "ok"|"degraded"|"down"|"unknown", "nodes_total": int, "nodes_healthy": int,
   "calls_per_second": int, "calls_history": [int x 60] }, "generated_at":
   ISO 8601 }`. `degraded` means at least one node is not `ok`; `down` means
   no healthy crawl node; `unknown` means no nodes have joined. One cheap call
   serves the rail so no other view has to be fetched for the chrome.
2. `PATCH /me` with `{ "locale": "en"|"vi" }` so the EN/VI toggle persists per
   user ([i18n](../i18n.md)). If JOOXF-79 lands before this exists, the toggle
   stores the choice in `localStorage` and the request is added later.
3. Non-admin calls to `/admin/*` answer 403 with code `forbidden` (already the
   `RequireAdmin` rule in `docs/SPEC.md`).

Errors: a 401 sends the user to login; 403 shows the forbidden page; any other
failure of `/admin/shell` shows the footer error state, never a page-level
block (the content region keeps working).

## Actions

| Action | Control | Result |
| --- | --- | --- |
| Go to a destination | Rail item (link) | Client route change; focus moves to the new view `h1`; the drawer closes on mobile |
| Open/close rail on mobile | Menu button, scrim, Esc, item click | Drawer opens/closes (see Responsive) |
| Switch User/Admin | Role switch (admins only) | Navigate to `/` or `/admin` |
| Pause/resume live | Live toggle | See Live updates |
| Switch language | EN/VI toggle | Sets `<html lang>`, re-renders copy, persists `locale` |
| Switch theme | Theme button | Toggles ADMIN dark/light, persisted |
| Open notifications | Bell | Opens the notifications drawer (overlay spec) |
| Open cluster nodes | Footer | Navigate to `/admin/nodes` |
| Retry shell data | Footer `Retry` | Refetches `/admin/shell` |
| Add a node (empty cluster) | Footer `Add a node` | Navigate to `/admin/nodes` |

## Interactions

See Actions. Keyboard: Tab order is skip link, top bar, rail, content. Within
the rail every item is a tab stop (links), Enter activates.

## States

Shell states are independent of the content region, which has its own states
per view.

### Ready

Rail with 14 items, current item highlighted, Issues badge (hidden at 0),
footer `Healthy`/`Degraded`/`Down` with counts and sparkline, Live indicator
green, bell count when unread above 0 (capped at `99+`).

### Loading

While `GET /me` or the first `GET /admin/shell` is pending: top bar and rail
render at once (labels are static copy, so no layout shift); the Issues badge
and the bell count are omitted; the footer shows a `skeleton` (pill 64 x 18,
text line 70 %, sparkline block 20 px). The content region shows its view's
skeleton. A guard that has not resolved `role` renders only the top bar and a
centred spinner, never the rail, so a non-admin never sees admin chrome flash.

### Empty

No node has joined (`cluster.status = unknown`, `nodes_total = 0`): footer pill
`No nodes`, text "No nodes joined", `Add a node` button to `/admin/nodes`, no
sparkline. Rail and badge behave normally. Empty is a footer state, not a page
state.

### Error

`GET /admin/shell` fails: the footer shows a compact `banner bad` "Cluster
status unavailable" with `Retry`; badge and bell count are hidden (never show a
stale number as current); the content region is unaffected. `GET /me` fails
with a non-401 error: full-width `banner bad` "Could not load the admin area."
with `Retry`, skeleton below at 35 % opacity, no rail items are links to data
until `role` is known.

### Partial and stale

If the last successful `/admin/shell` is older than 20 s (two poll intervals)
or the live channel is down, the footer pill gains a `Stale` pill and the line
"Updated {age}". Numbers stay visible, and the Issues badge keeps its last value. The Live
toggle shows `Paused` with the reason in its title. The Stale marker is the
only signal; no other element changes.

### Forbidden

Role `user` (or a 403 from any admin call): the 403 page described in Route
and access, in the public layout, in the user's own theme. Title "You do not
have access to this page", text "This page is for administrators. Your account
has the User role.", CTA `Go to home`.

## Live updates

- Live channels (one SSE connection per tab,
  `GET /api/v1/events/stream`): `nodes.health` (footer status, counts, calls
  per second), `events.issues` (Issues badge), `notifications.me` (bell).
  Payload and `Last-Event-ID` rules are in [patterns](../patterns.md).
- Footer sparkline: the server appends one point per minute; the client shifts
  the array on each `nodes.health` `snapshot`.
- Polling fallback (SSE blocked, error or 3 failed reconnects): poll
  `GET /admin/shell` every 10 s, backoff 10, 20, 40 s, max 60 s; stops when the
  tab is hidden and does one fetch on return.
- The Live toggle (dot plus word `Live`/`Paused`, `aria-pressed`) is part of
  this shell. Paused by the user buffers events (max 200) and applies them on
  resume. Status changes are announced once through the polite live region
  ("Live data paused", "Live data resumed", "Cluster degraded"). Footer
  changes do not move focus.
- Reduced motion: no pulse on the live dot, no `fresh` animation on the badge.

## Responsive

| Width | Behaviour |
| --- | --- |
| Above 1100 px | Rail 226 px, content max width fluid |
| 901 to 1100 px | Same rail; content grids collapse by their views |
| 900 px and below | Rail becomes an off-canvas drawer (250 px, left, `translateX(-105%)`, 0.2 s), menu button appears, grid is one column |
| 720 px and below | Top bar wraps; profile avatar and the role switch label text are hidden (icons stay); targets stay at least 40 px |

- Drawer behaves as a modal: `role="dialog"`, `aria-modal="true"`, labelled
  "Admin sections", focus moves to the first item, Tab is trapped, Esc, the
  scrim and choosing an item close it and return focus to the menu button.
  The scrim covers the content; background content is `inert`.
- On drawer open the cluster footer stays at the bottom of the drawer; safe-area
  insets apply on the left and top. Verified widths: 1440 and 390 px.

## Accessibility

- Landmarks: `header` (top bar), `nav aria-label="Admin sections"`, one
  `main id="main"`, skip link "Skip to content" first in the tab order.
- Current page: `aria-current="page"`; the Issues badge is read as part of the
  link name ("Issues, 3 open"). Group labels are not links; each group is a
  `role="group"` labelled by its eyebrow.
- Icons are `aria-hidden`; icon buttons carry `aria-label` in the active
  language; cluster status is text (`Healthy`), not colour only. Sparkline has
  a text alternative: "Cluster calls per second, last 60 minutes, now {calls}".
- Focus: 2 px accent outline, 2 px offset, never hidden under the sticky top
  bar (`scroll-padding-top: 64px`). After a route change focus moves to the
  view `h1`.
- Contrast per [design system](../design-system.md) ADMIN tokens (corrected
  values, not POC values). Respect `prefers-reduced-motion`.

## i18n

Keys follow `area.view.element`. English is the source; Vietnamese keeps
`Flickr`, `XFlickr`, `API`, node names and numbers. Counts use ICU plural.

| Key | EN | VI |
| --- | --- | --- |
| `shell.skip-link` | Skip to content | Bỏ qua đến nội dung |
| `shell.menu.open` | Open menu | Mở menu |
| `shell.menu.close` | Close menu | Đóng menu |
| `shell.role.group` | Role | Vai trò |
| `shell.role.user` | User | Người dùng |
| `shell.role.admin` | Admin | Quản trị |
| `shell.live.on` | Live | Trực tiếp |
| `shell.live.paused` | Paused | Tạm dừng |
| `shell.live.paused-toast` | Live data paused | Đã tạm dừng dữ liệu trực tiếp |
| `shell.live.resumed-toast` | Live data resumed | Đã tiếp tục dữ liệu trực tiếp |
| `shell.lang.toggle` | English / Tiếng Việt | English / Tiếng Việt |
| `shell.theme.toggle` | Switch light or dark theme | Chuyển giao diện sáng hoặc tối |
| `shell.notifications` | Notifications | Thông báo |
| `shell.profile` | Profile | Hồ sơ |
| `admin.nav.label` | Admin sections | Các mục quản trị |
| `admin.nav.group.operations` | Operations | Vận hành |
| `admin.nav.group.pipelines` | Pipelines | Luồng xử lý |
| `admin.nav.group.data` | Data & people | Dữ liệu & người dùng |
| `admin.nav.overview` | Overview | Tổng quan |
| `admin.nav.nodes` | Cluster nodes | Các node |
| `admin.nav.quota` | API quota | Hạn mức API |
| `admin.nav.metrics` | Metrics | Số liệu |
| `admin.nav.events` | Events & audit | Sự kiện & kiểm toán |
| `admin.nav.issues` | Issues | Sự cố |
| `admin.nav.issues-count` | {count, plural, one {Issues, # open} other {Issues, # open}} | Sự cố, {count, plural, other {# đang mở}} |
| `admin.nav.pipeline` | Crawl pipeline | Luồng thu thập |
| `admin.nav.downloads` | Downloads | Tải xuống |
| `admin.nav.sync` | Cloud sync | Đồng bộ đám mây |
| `admin.nav.ai` | AI index | Chỉ mục AI |
| `admin.nav.users` | Users | Người dùng |
| `admin.nav.defaults` | Defaults | Mặc định |
| `admin.nav.sharing` | Sharing | Chia sẻ |
| `admin.nav.log` | API call log | Nhật ký gọi API |
| `admin.shell.cluster.title` | Cluster | Cụm |
| `admin.shell.cluster.healthy` | Healthy | Khỏe mạnh |
| `admin.shell.cluster.degraded` | Degraded | Suy giảm |
| `admin.shell.cluster.down` | Down | Ngừng hoạt động |
| `admin.shell.cluster.summary` | {calls} calls/s · {healthy}/{total} nodes | {calls} lệnh gọi/giây · {healthy}/{total} node |
| `admin.shell.cluster.chart-alt` | Cluster calls per second, last 60 minutes, now {calls} | Số lệnh gọi mỗi giây của cụm trong 60 phút qua, hiện tại {calls} |
| `admin.shell.cluster.stale` | Stale | Cũ |
| `admin.shell.cluster.updated` | Updated {age} | Cập nhật {age} |
| `admin.shell.loading.text` | Loading cluster status… | Đang tải trạng thái cụm… |
| `admin.shell.empty.title` | No nodes joined | Chưa có node nào tham gia |
| `admin.shell.empty.text` | Start the node agent on a VM with a join token. | Chạy node agent trên một máy chủ ảo với mã tham gia. |
| `admin.shell.empty.cta` | Add a node | Thêm node |
| `admin.shell.error.title` | Cluster status unavailable | Không có trạng thái cụm |
| `admin.shell.error.text` | The control node did not answer within 10 s. | Node điều khiển không phản hồi trong 10 giây. |
| `admin.shell.me-error.title` | Could not load the admin area. | Không thể tải khu vực quản trị. |
| `admin.shell.me-error.text` | Your session could not be checked. Try again. | Không thể kiểm tra phiên của bạn. Hãy thử lại. |
| `common.retry` | Retry | Thử lại |
| `error.forbidden.title` | You do not have access to this page | Bạn không có quyền truy cập trang này |
| `error.forbidden.text` | This page is for administrators. Your account has the User role. | Trang này dành cho quản trị viên. Tài khoản của bạn có vai trò Người dùng. |
| `error.forbidden.cta` | Go to home | Về trang chủ |

## Test IDs

Test hooks are listed below; E2E specs (`web/e2e/admin-shell.spec.ts`) use
only these `data-testid` values.

| Test id | Element |
| --- | --- |
| `admin-shell` | Shell root (`data-role="admin"`) |
| `admin-topbar` | Top bar |
| `admin-menu-btn` | Mobile menu button |
| `admin-role-switch` | Role switch group (admins only) |
| `admin-live-toggle` | Live/Paused toggle |
| `admin-lang-toggle` | EN/VI toggle |
| `admin-theme-toggle` | Theme button |
| `admin-bell` | Notifications button |
| `admin-bell-count` | Unread count |
| `admin-rail` | Rail `nav` (drawer on mobile) |
| `admin-rail-scrim` | Mobile drawer scrim |
| `admin-nav-<id>` | Rail item; ids `overview`, `nodes`, `quota`, `metrics`, `events`, `issues`, `pipeline`, `downloads`, `sync`, `ai`, `users`, `defaults`, `sharing`, `log` |
| `admin-nav-issues-badge` | Open issues badge |
| `admin-cluster-footer` | Cluster footer |
| `admin-cluster-status` | Footer status pill |
| `admin-cluster-summary` | Calls and nodes line |
| `admin-cluster-spark` | Footer sparkline |
| `admin-cluster-loading` | Footer skeleton |
| `admin-cluster-empty` | Footer empty block |
| `admin-cluster-error` | Footer error banner |
| `admin-cluster-retry` | Footer retry button |
| `admin-cluster-stale` | Stale pill |
| `admin-main` | Content region |
| `admin-forbidden` | 403 page |
| `admin-forbidden-cta` | 403 `Go to home` button |

## Out of scope

The content of every destination view, the notifications drawer, sign-out, the
profile page, login, the setup wizard, and application code and OpenAPI edits
(the two contract requests above are requests only). The POC's WireGuard
addresses and foreign regions are not part of the product.

## Test hooks

The `data-testid` values are listed under Test IDs above: `admin-shell`,
`admin-rail`, `admin-nav-overview`, `admin-cluster-footer`, `admin-forbidden`.
