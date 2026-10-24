# XFlickr cross-cutting UX patterns

These patterns apply to every screen of both roles (USER and ADMIN). A view
specification may add content to a pattern but may not change its behaviour.
Tokens come from [`design-system.md`](design-system.md) and
[`tokens.md`](tokens.md). Accessibility rules are in [`a11y.md`](a11y.md) and
copy rules are in [`i18n.md`](i18n.md). Baseline behaviour is the POC
(`poc/xflickr-poc.html`): the Preview control (section 9, states) and the live
simulation and router (section 12, boot and tick loop).

## Loading skeletons

- A view that has no data yet shows a skeleton, never a blank page and never a
  spinner alone. The page head (title, actions) renders at once.
- Shape: the skeleton mirrors the final layout. USER photo views use a justified
  grid of placeholder tiles with the real aspect ratios when known (POC ratios
  `1.5, .75, 1.33, 1`). ADMIN views use a row of stat blocks and a panel of list
  rows.
- Skeleton blocks use the `loading` motion token (1.2s shimmer). Under reduced
  motion they are static sunken blocks.
- Show a line `Loading <view name>…` with the `spin` spinner above the
  skeleton. The container has `aria-busy="true"` and the line is the only
  announcement (`role="status"`).
- Show the skeleton only if the request has not answered within 200 ms, so fast
  responses do not flash. Once shown, keep it for at least 400 ms.
- After 10 s without an answer the view switches to the error state.
- Refreshing a view that already has data keeps the data on screen and shows a
  small spinner next to the page title. Never replace data with a skeleton on
  refresh.

## Empty states

An empty state has four parts in this order: icon, title, text, and an optional
call to action (CTA).

- Icon: one icon from the design-system inventory at 30px in a 72px circular
  sunken `art` block, color `faint`.
- Title: `h3`, sentence case, says what is missing ("No contacts yet").
- Text: one or two sentences, max width 46ch, says why and what happens next.
- CTA: at most one primary action, only when the user can fix the emptiness.
  No CTA when the system fills the view by itself.
- Distinguish "nothing yet" (first use) from "nothing matches" (filters or
  search). The second says which filter to change and offers `Clear filters`.

### Default copy table

The key is `area.view.empty.*` (see [`i18n.md`](i18n.md)). A view may override
the text with a more specific sentence but keeps the icon and the tone.

| View | Icon | Title | Text | CTA |
| --- | --- | --- | --- | --- |
| home | image | Nothing crawled yet | Connect a Flickr account to start. Your contacts appear first, then their photos. | Connect account |
| library | image | Your library is empty | Photos appear here as soon as the crawler stores them. | — |
| contacts | users | No contacts yet | Contacts of your Flickr accounts show up after the first crawl pass. | — |
| albums | album | No albums yet | Albums of your contacts appear after their photo lists are crawled. | — |
| dl | download | No downloads yet | Pick a contact or an album and choose Download. | Browse contacts |
| sync | cloud | No cloud connected | Connect Google Photos, Drive, OneDrive, Dropbox or S3 to send downloads there. | Connect a cloud |
| keys | key | No API key yet | Add your Flickr API key and secret to start crawling. | Add key |
| nodes | server | No nodes joined | Start the node agent on a VM with a join token. | Add node |
| issues | check | No issues | Nothing needs a person right now. | — |
| events | list | No events yet | Events stream in as soon as the scheduler runs. | — |
| adl | download | No download jobs | Jobs appear when users start downloads. | — |
| async | cloud | No uploads | No user has connected a cloud provider yet. | — |
| log | eye | No API calls logged | Calls appear once a key is active. | — |
| pipeline | activity | Queue is empty | Every contact is crawled and checked. New work arrives with the next change check. | — |
| search (no query) | sparkle | Describe what you remember | Try a description of the photo in English or Vietnamese. | — |
| any filtered list | info | No results match | Change the filters to see more. | Clear filters |
| fallback | info | Nothing here yet | This view fills up once the system has data for it. | — |

## Error states

- A failed load replaces the content with a `banner bad`: alert icon, bold
  sentence "Could not load <view name>.", one line of cause and consequence
  ("The control node did not answer within 10 s. Data shown elsewhere may be
  stale."), and a `Retry` button.
- The skeleton of the view stays below the banner at 35% opacity and is not
  interactive, so the layout does not jump on retry.
- `Retry` repeats the failed request, shows the loading state, and moves focus
  to the banner heading on a second failure. Retry is never automatic for user
  initiated actions; background polling retries with backoff (see Live
  updates).
- Messages name the thing that failed and the next step. Never show a raw
  status code, stack trace, or request id as the only text. The request id may
  appear as small mono text for support.
- Field errors appear under the field, linked with `aria-describedby`, and are
  summarised in an error banner at the top of a form with more than three
  fields.
- A failed action (save, pause, retry job) shows an error toast with the reason
  and keeps the control enabled.

## Partial and stale data markers

- Stale: when the last successful fetch of a block is older than twice its
  refresh interval, or a live channel is down, show a `Stale` pill with the age
  ("Updated 3 min ago") next to the block title. Data stays visible.
- Partial: when one block of a view fails while others load, only that block
  shows the error banner (inline, compact) with its own `Retry`. The rest of the
  view stays usable.
- Incomplete data: numbers that are still being counted (crawl in progress)
  show `≥` or `~` plus a small progress hint, never a final-looking value.
- Never hide a marker behind a tooltip. The marker has text, not only color.

## Forbidden and not-found pages

- 403 Forbidden (signed in, wrong role, `forbidden` code): full-page message
  with lock icon, title "You do not have access to this page", text "This page
  is for administrators. Your account has the User role.", CTA `Go to home`.
  Do not reveal whether the resource exists.
- 404 Not found (unknown route or id): full-page message with search icon, title
  "We could not find that page", text "The link may be old, or the item was
  removed.", CTA `Go to home` and a secondary `Back`.
- A private or unshared item uses the 404 page for other users, not 403.
- Both pages keep the app shell (top bar, rail) so the user can navigate away.
  Document title becomes "Access denied" or "Page not found".
- The router sends unknown POC-style hashes to the role home view.

## Session expiry

- A 401 from any API call means the session is gone (14-day sliding expiry, see
  `docs/sessions.md`). Stop all polling and live channels at once.
- Show a modal "Your session ended" with text "Sign in again to continue. Your
  place is saved." and a `Sign in` button. Focus moves into the modal.
- The sign-in screen returns the user to the route they were on (`return_to` is
  a same-origin path only).
- Unsaved form input is kept in memory while the modal is open. After sign-in
  the form is restored and submitted again by the user, never automatically.
- A 403 with code `csrf_rejected` refreshes the CSRF token once and repeats
  the request. If it fails again it is shown as an error toast.

## Offline mode

- Detect with `navigator.onLine`, the `online` and `offline` events, and failed
  requests (network error is not an HTTP status).
- Offline shows a persistent top banner "You are offline. Showing the last data
  we have." with `info` styling. It has no dismiss button and disappears when
  the connection returns, followed by a toast "Back online".
- Offline: live channels pause, indicator shows `Paused` with reason, reads
  come from the in-memory view cache and carry the Stale marker, and all
  write actions are disabled with a title "Needs a connection".
- On return, channels reconnect, views refetch once, and the banner clears. Do
  not queue writes for later replay.

## Live updates

### SSE channel naming

- One SSE connection per tab: `GET /api/v1/events/stream`. The server
  multiplexes channels over it. Channel names are lowercase, dot separated:
  `area.entity[.id]`.
- Names in use: `crawl.jobs`, `crawl.pipeline`, `downloads.jobs`,
  `downloads.job.<id>`, `cloud.uploads`, `nodes.health`, `nodes.node.<id>`,
  `quota.keys`, `events.audit`, `events.issues`, `calls.log`, `ai.index`,
  `notifications.me`.
- A view subscribes on mount and unsubscribes on unmount. USER channels are
  scoped by the server to the signed-in user; ADMIN channels require the admin
  role and answer 403 otherwise.
- Event payload: `{ "channel", "type", "id", "at", "data" }`. `type` is
  `upsert`, `remove`, or `snapshot`. The client ignores unknown types.
- Use `Last-Event-ID` on reconnect so no event is lost within the server
  buffer. If the server answers `snapshot`, replace the block data.

### Polling fallback intervals

When SSE is unavailable (blocked, error, 3 failed reconnects) the view polls
the matching REST endpoint. Polling stops when the tab is hidden and resumes
with one immediate fetch.

| Data | Interval | Backoff on failure |
| --- | --- | --- |
| Download job progress, running crawl jobs | 2 s | 2 s, 4 s, 8 s, max 30 s |
| Events stream, API call log, issues | 5 s | 5 s, 10 s, 20 s, max 60 s |
| Node health, quota, pipeline, overview stats | 10 s | 10 s, 20 s, 40 s, max 60 s |
| Lists without live meaning (contacts, albums) | none | refetch on focus only |

SSE reconnect backoff is 1 s, 2 s, 4 s, 8 s, then 15 s, with 20% jitter.

### Live and Paused indicator

- The top bar shows one toggle button: a dot plus the word `Live` or `Paused`.
  `aria-pressed` is true when live. The word, not color alone, carries state.
- `Live` (green dot with glow): channels connected or polling works.
  `Paused`: the user paused, the tab is hidden, the device is offline, or
  channels failed.
- Paused by the user stops applying updates. Incoming events are buffered (max
  200) and a button "N new" appears at the top of the affected list. Resuming
  applies the buffer and refetches.
- Paused by the system shows the reason in the title ("Offline", "Reconnecting
  in 4 s") and in the Stale marker. The user can still toggle; toggling while
  the system pause is active retries the connection now.
- The choice is kept for the tab session only. Toggling shows a toast "Live
  data paused" or "Live data resumed".
- `Live` is announced once when it changes, through the polite live region
  (see [`a11y.md`](a11y.md)).

### New row animation

- A row that arrives live is inserted at the top (events, calls) or its place
  (jobs) with the `fresh` motion: background from `accent-soft` to the resting
  background over 1.6s, ease-out, once.
- A row is fresh for 1.6s after its event time. Rows loaded by the first fetch
  or a snapshot never animate.
- Updates to an existing row change values in place with no animation, except a
  state change, which re-triggers `fresh` once.
- If the user has scrolled down, new rows are not inserted above the viewport.
  Show "N new" and insert when clicked. If the user is at the top, insert
  directly.
- Cap lists at 300 live rows in memory and render at most 120. Removed rows
  disappear without animation.
- Reduced motion: no animation; a fresh row keeps a 1.6s `accent-soft` marker
  that is dropped without fade.

### Toast rules

- Toasts confirm an action or announce a result of the user's own action.
  Background events go to the Notifications drawer and the events list, not to
  toasts.
- One line, sentence case, under 90 characters, no trailing period unless there
  are two sentences. Examples: "Link copied", "Retry scheduled now".
- Position: bottom center above the safe area and any bottom control (64px
  offset). Stack up to 3 with an 8px gap; the oldest is dropped when a fourth
  arrives.
- Duration: 2.6 s for confirmations (POC), 6 s for errors, and errors stay
  while hovered or focused. Never auto-dismiss a toast that carries an action;
  such a toast closes on action or on `Esc`.
- Toasts that offer `Undo` last 8 s.
- Container is `aria-live="polite"`. Error toasts use `role="alert"`.
- Entrance uses the `entrance` token (translateY 8px to 0, opacity 0 to 1).
- Never use a toast for blocking errors, validation, or information the user
  must act on; use a banner or modal.

## Responsive breakpoints

Breakpoints are max-width queries from the POC: 1100, 900, and 720 px. Content
is designed for 1100 and up, then degrades. All values are px of viewport width.

| Breakpoint | What changes |
| --- | --- |
| ≥ 1101 | Full layout. ADMIN fixed left rail (250px) and content. Four-column stat grids (`g4`), three-column grids (`g3`). Lightbox is photo plus a side panel of 380px or more. USER main is max 1440px wide. |
| ≤ 1100 | `g4` and `g3` become two columns. Lightbox side panel narrows to 340px. |
| ≤ 900 | ADMIN rail becomes an off-canvas drawer (250px) opened by a menu button, with a scrim. Two-column grids (`g2`, `g21`, hero, onboarding) become one column. Library filters move above the content as a wrapping row. Lightbox stacks photo above panel and scrolls. Depth columns become two. |
| ≤ 720 | Top bar wraps; search takes its own full-width row (40px high). Elements marked `hide-sm` are hidden. `g4` becomes one column. Event rows drop the source column. The row-height token `--row-h` goes from 190px to 120px (see design-system). Tables reflow to labelled cards or scroll horizontally inside their container. |

Rules at every width:

- No horizontal page scroll. Wide tables and code blocks scroll inside their
  own container with a visible focus ring.
- Touch targets are at least 40 × 40 px (including the padding of icon buttons,
  chips, rows, toggles, and checkboxes) and at least 8 px apart. Where the
  visual control is smaller, enlarge the hit area with padding, not with
  invisible overlapping elements.
- Text never shrinks to fit; it wraps (see design-system localization).
- Off-canvas rails and drawers trap focus and close on `Esc`, scrim click, and
  route change.
- Safe-area insets apply to the top bar, the toast stack, and fixed controls.
- Do not rely on hover. Every hover reveal (tray button, tooltip) has a touch
  and keyboard equivalent.
- Verify at 1280, 1024, 800, 600, and 360 px wide, and at 200% zoom (reflow at
  320 CSS px).

## Data-testid naming convention

`data-testid` is for E2E tests only. It never changes styling or behaviour.

- Format: `area-view-element` in lowercase kebab-case, matching the i18n key
  `area.view.element` with dots turned into hyphens. Example: key
  `admin.nodes.add-button` has testid `admin-nodes-add-button`.
- Areas: `user`, `admin`, `shell` (top bar, rail, toasts), `auth`, `common`
  (shared components such as empty state and banner).
- Repeated items get a stable id suffix: `user-contacts-card-<nsid>`,
  `admin-nodes-row-<nodeId>`. Never use an array index.
- States use a fixed suffix: `-skeleton`, `-empty`, `-error`, `-retry`.
  Example: `user-library-empty`, `admin-events-error-retry`.
- Shared elements: `shell-live-toggle`, `shell-lang-toggle`, `shell-toast`,
  `shell-notifications-button`, `common-banner`, `common-modal`,
  `common-drawer`, `common-lightbox`, `shell-offline-banner`.
- Dialogs: root `<area>-<view>-<name>-modal`, close `...-modal-close`, confirm
  `...-modal-confirm`.
- Forms: field `<area>-<view>-<form>-<field>`, submit `...-submit`, error
  `...-<field>-error`.
- Prefer role and accessible name selectors first; use testid when text or
  role is unstable (for example language-dependent labels). Tests never select
  by translated text.
- A testid is unique on the page and never contains user data such as names,
  titles, or secrets.
