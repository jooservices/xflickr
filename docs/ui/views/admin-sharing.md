# Admin sharing

## Purpose

Give administrators a read-only, aggregate view of the sharing pool: how many
eligible accounts have opted in and how many eligible public records are
currently reusable by other XFlickr users. Explain the consent and privacy
boundary. Never expose private Flickr records or identify a contributing user.

## Route and access

- Route: /admin/sharing in the authenticated ADMIN shell, with ADMIN dark
  control-room theme by default and the existing ADMIN light theme as an option.
- A 401 follows the shell sign-in flow. A 403 clears the page data and shows the
  shared forbidden state.
- The page is read-only. An account owner changes their own choice on /sharing;
  this page has no user toggle, bulk switch, account drill-down or write action.
- Do not use USER-scoped data or events to populate an ADMIN view.

## Layout

- Page heading: Sharing; explain that only eligible public records from users
  who explicitly opted in can be reused.
- Three aggregate cards: users sharing (count and total active USER
  accounts), eligible public records currently visible through sharing, and the
  snapshot time. Show an unavailable value as an em dash, never as zero.
- A policy panel lists what can be reused and what is never shared. Do not
  display a numeric count of excluded private records.
- No per-user table, names, emails, account identifiers, record links or
  record-level drill-down. Keep Refresh visible in the page header.
- POC sample values are illustrative. The POC per-user table and its “Private
  records” and “Calls saved today” figures are not production requirements:
  individual rows cross the aggregate privacy boundary, private-record counts
  reveal private activity, and no authoritative call-savings metric exists.

## Data

This page consumes one server-computed snapshot. Counts must be authoritative,
deduplicated, and clearly scoped to active USER accounts and eligible public
records. The client must not derive shared counts from user rows, crawl history,
API-key totals, Flickr calls, or mock values.

A shared record is a distinct record that is public under Flickr visibility
rules and has at least one active USER observer whose sharing choice is on.
Friends-only, family-only, private, deleted/tombstoned and otherwise
unauthorized records are excluded. Sharing is off by default. A user who opts
out contributes no records; their observations remain available only through
their own access. These rules follow docs/SPEC.md and the sharing domain policy.

## Data and API

All paths are relative to /api/v1. This design ticket documents requirements;
it does not edit OpenAPI or application code.

| Content | Source, fields and action |
| --- | --- |
| Aggregate summary | GET /admin/sharing is a contract request. ADMIN-only response: active_user_account_count (integer), sharing_enabled_user_count (integer), shared_public_record_count (integer or null when unavailable), user_account_snapshot_at (date-time), and record_snapshot_at (date-time or null). Enabled USER count cannot exceed active USER count. A null or stale record count means unavailable, not zero. Return aggregates only; no per-account state or record identifiers. |
| Current USER choice | PUT /me/sharing exists and changes only the authenticated user’s own choice. This ADMIN page never calls it. GET /me/sharing does not exist in the current contract and must not be inferred from PUT. |
| Live summary | GET /admin/sharing/events is a contract request for an ADMIN-only resumable SSE stream using Last-Event-ID. Emit admin.sharing.summary.updated with only event id and observation time; the client refetches GET /admin/sharing. Do not include counts, user references, records or visibility data in the event. |
| Existing events | GET /events exists but is USER-scoped and carries user progress. It is not an ADMIN sharing feed and must not be used here. |
| Errors | Use the shared Error envelope. Localize stable codes; never render the server message. A 401 signs out, 403 clears data, and 429/network/5xx preserve last-good values and offer Retry. |

The proposed endpoint must not return usernames, emails, user or account IDs,
Flickr NSIDs, record IDs, titles, photo URLs, visibility attributes, keys,
secrets, tokens, downloads or per-user contribution counts. It must not return
a count of excluded private records. The browser receives no hidden record data.

## Actions

- Refresh requests a new aggregate snapshot and preserves the last-good values
  while loading.
- Open Users navigates to /admin/users; it does not carry sharing state in the
  URL or select a user.
- No action changes an account’s consent. Do not add an ADMIN override, bulk
  enable/disable, per-user state view, or private-record inspection.

## Interactions

On route entry, fetch GET /admin/sharing. Render only validated server fields.
Refresh and successful live events fetch a new snapshot. Ignore event payload
fields except event id and observation time. Never optimistically change a
sharing choice or count. When no eligible records exist, show zero only when
the server explicitly returns zero. If the field is null or absent, show
unavailable and preserve its freshness state.

## States

### Ready

This is the normal state. Show account counts, shared public record count and
their snapshot freshness. Pair each count with a text label and keep the policy
panel visible.

### Loading

For an initial response taking more than 200ms, show skeleton cards with
aria-busy=true and one polite status announcement. During refresh retain
last-good values and mark the cards busy; do not blank or replace them with
zeroes.

### Empty

When the server returns zero eligible active USER accounts or zero eligible shared
records, show “No sharing activity yet” with an explanation that sharing is
opt-in and only eligible public records appear. CTA “Open Users” navigates to
/admin/users. Do not invite an administrator to enable sharing for someone.

### Error

If the initial request fails, show “Could not load sharing summary”, explain
that no setting was changed, and offer Retry. A failed refresh retains the
last-good snapshot with a stale label. Never present a failed request as zero.

### Partial and stale

Account counts and record counts have independent snapshot times. If either
metric is null or older than 60 seconds, mark only that card unavailable or
stale, keep valid values visible, and offer Refresh. A stale count must not be
described as current. Missing required fields or an invalid count relation is
a response error, not a partial success.

### Forbidden

For 401, follow shell expiry and close the event stream. For 403, clear all
cached counts and show the shared access-denied state without revealing
whether sharing activity exists. No record or account information remains in
the page or URL.

## Live updates

Use the proposed ADMIN-only GET /admin/sharing/events stream and resume with
Last-Event-ID. After admin.sharing.summary.updated, fetch the aggregate
snapshot; never trust event data as a count. If the stream is unavailable or
reconnects fail three times, poll GET /admin/sharing every 30 seconds with
backoff capped at two minutes. Pause polling while the document is hidden and
refresh on return. Mark a metric stale 60 seconds after its last successful
snapshot. Stop polling and close SSE on sign-out or 403. Until the ADMIN stream
contract exists, use Refresh only; USER GET /events is not a fallback.

## Responsive

At 1440px use three cards in one row above the policy panel. At 720px and
below stack the cards in two columns, then one column at 390px. Keep the policy
copy readable without horizontal scrolling. Use 16px mobile gutters and allow
English and Vietnamese labels to wrap without clipping.

## Accessibility

Use one h1, semantic sections and definition lists for aggregate metrics.
Provide accessible names for Refresh and Open Users; expose loading and stale
changes through a polite live region. Mark unavailable values in text, not
color alone. Preserve visible keyboard focus, 4.5:1 text contrast, logical
focus order and reduced-motion preferences. The empty CTA and retry action
must be keyboard operable.

## i18n

Provide EN and VI copy for every label, policy sentence, empty/loading/error
message, stale status and action. Interpolate {count}, {total} and {time}.
Never render raw server messages.

| Key | EN / VI |
| --- | --- |
| admin.sharing.title | Sharing / Chia sẻ |
| admin.sharing.description | Opt-in public records, safely reused across XFlickr. / Hồ sơ công khai được chia sẻ tự nguyện và tái sử dụng an toàn trong XFlickr. |
| admin.sharing.live | Live / Đang cập nhật |
| admin.sharing.stale | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| admin.sharing.action.refresh | Refresh / Làm mới |
| admin.sharing.action.openUsers | Open Users / Mở Người dùng |
| admin.sharing.metric.accounts | Users sharing / Người dùng đang chia sẻ |
| admin.sharing.metric.accountsTotal | {count} of {total} active users / {count} trên {total} người dùng đang hoạt động |
| admin.sharing.metric.records | Eligible public records shared / Hồ sơ công khai đủ điều kiện đang được chia sẻ |
| admin.sharing.metric.snapshot | Snapshot updated / Thời điểm cập nhật ảnh chụp dữ liệu |
| admin.sharing.metric.snapshotTime | {time} ago / {time} trước |
| admin.sharing.metric.unavailable | Unavailable / Không có dữ liệu |
| admin.sharing.policy.title | Sharing and privacy / Chia sẻ và quyền riêng tư |
| admin.sharing.policy.optIn | Only public records from accounts that opted in can be reused. / Chỉ hồ sơ công khai từ tài khoản đã tự nguyện tham gia mới được tái sử dụng. |
| admin.sharing.policy.excluded | Private, friends-only and family-only records are never shared. / Hồ sơ riêng tư, chỉ bạn bè và chỉ gia đình không bao giờ được chia sẻ. |
| admin.sharing.policy.never | API keys, secrets, tokens and downloads are never shared. / Khóa API, bí mật, token và nội dung tải xuống không bao giờ được chia sẻ. |
| admin.sharing.policy.aggregate | This page shows aggregate counts only. / Trang này chỉ hiển thị số liệu tổng hợp. |
| admin.sharing.loading | Loading sharing summary / Đang tải tổng quan chia sẻ |
| admin.sharing.empty.title | No sharing activity yet / Chưa có hoạt động chia sẻ |
| admin.sharing.empty.body | Sharing is optional. Eligible public records appear after an account opts in. / Chia sẻ là tùy chọn. Hồ sơ công khai đủ điều kiện sẽ xuất hiện sau khi tài khoản tự nguyện tham gia. |
| admin.sharing.error.title | Could not load sharing summary / Không thể tải tổng quan chia sẻ |
| admin.sharing.error.body | No sharing setting was changed. Try again. / Không có tùy chọn chia sẻ nào bị thay đổi. Hãy thử lại. |
| admin.sharing.forbidden.title | You do not have access to this page / Bạn không có quyền truy cập trang này |
| admin.sharing.forbidden.body | This page is for administrators. / Trang này chỉ dành cho quản trị viên. |
| admin.sharing.retry | Retry / Thử lại |

## Test IDs

| Element | data-testid |
| --- | --- |
| Page | admin-sharing-page |
| Summary | admin-sharing-summary |
| Active account metric | admin-sharing-accounts |
| Shared record metric | admin-sharing-records |
| Policy | admin-sharing-policy |
| Refresh | admin-sharing-refresh |
| Empty state | admin-sharing-empty |
| Error state | admin-sharing-error |
| Loading state | admin-sharing-loading |
| Forbidden state | admin-sharing-forbidden |
| Stale indicator | admin-sharing-stale |
| Open Users action | admin-sharing-open-users |

## Test hooks

Keep the IDs above stable across states and themes. Verify the page renders only
the aggregate contract, that null is not rendered as zero, and that USER-scoped
events, per-account data and private-record counts are not requested or shown.

## Out of scope

Application code, OpenAPI changes, per-user or per-record views, sharing-choice
mutations, administrative overrides, numeric private-record counts, and
“calls saved” estimates until an authoritative metric contract exists. The POC
is a visual reference only; its synthetic per-user rows and sample totals are
not production data.
