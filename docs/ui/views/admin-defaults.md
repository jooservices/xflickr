# Admin defaults and plans

## Purpose

Give ADMIN one place to inspect and manage global application defaults, plan entitlements, and feature-flag rollout settings. Follow the Defaults view in the approved POC and the ADMIN control-room design system. POC values are illustrations; the UI always renders values returned by the API.

## Route and access

- Route: /admin/defaults in the authenticated ADMIN shell. ADMIN defaults to the dark control-room theme; the optional light theme keeps the same compact geometry.
- A 401 follows the shell sign-in flow. A 403 clears page data and shows the shared forbidden state.
- All reads and writes are ADMIN-only. Writes require CSRF, a staged diff, an audit note, server validation, and a durable audit result before the page reports success.
- Do not show individual users, their settings, API keys, secrets, or user IDs. The effective value on this page is the validated application default before any per-user override; users may have different effective settings.

## Layout

1. Page header: Defaults and plans, concise description, last-updated time, and Refresh.
2. Global defaults: grouped fields for crawl, quota, downloads, and retries. Each row shows the setting name, source value, effective default, unit, server-provided bounds, and whether a user override is allowed.
3. Plans: table with name, scheduler weight, depth/contact caps, AI allowance per day, ZIP retention, and resource share. On narrow screens render one plan per card.
4. Feature flags: name, enabled state, and audience label. For user_ids audiences show “Specific users” only; never render the IDs.
5. Save bar: changed-field count, a required audit-note field, Save changes, and Discard. Save opens a confirmation summary for global quota, plan, and rollout changes; it never applies optimistically.

## Data

Render only server responses. Do not infer an effective default from a POC sample, a client constant, a plan name, an environment variable, or a per-user endpoint. The configured application value and its source are distinct from a user's merged effective setting. When an API cannot provide either value, show unavailable and keep Save disabled for that item.

Illustrative baseline shown in the mockup: max depth 2 with cap 4; hourly ceiling 3,600 calls; reserve 100; per-second rate 4 and burst bucket 5; person lane 30% and photo lane 70%; ZIP TTL 24 hours; retry maximum 5 with 30-second initial backoff; node cooldown 45 minutes; change-check interval 24 hours; group pools off. These values are grounded in the settings registry and product docs where available. The POC’s burst field does not override the backend setting of 5. Sample values and plan/flag rows are not a live configuration snapshot.

## Data and API

All paths are relative to /api/v1. The design ticket does not change application code or OpenAPI.

| Content | Current source, fields and action |
| --- | --- |
| Defaults and caps | Contract request: ADMIN-only GET /admin/defaults returns revision, updated_at, and items[] with key, source_value, effective_value, source_kind, unit, user_overridable, min, max, and updated_at. effective_value means the validated global application default before user overrides. The source kind distinguishes seeded from administrator-configured values. The existing app_settings registry includes max_depth, change_check_interval, daily_call_limit, quota_reserve, per_second_rate, burst, person_lane_share, zip_ttl, retry_max, retry_backoff, node_cooldown, and group_pools_enabled. There is no current ADMIN route for it; /me/crawl/settings is user-scoped and must not be reused. |
| Save defaults | Contract request: ADMIN-only PUT /admin/defaults accepts expected_revision, audit_note, and changed setting definitions (key, value, and only the policy fields the server marks editable, such as max for max_depth); validates an allowlist and immutable hard bounds; writes settings and durable audit evidence atomically; and returns the new revision and snapshot. A stale revision returns 409. A cap change that would invalidate an existing user override is rejected without mutation and without exposing user identity. Validation or audit failure changes nothing and returns an error. The settings repository currently publishes settings.changed transactionally, but that is not a substitute for an ADMIN audit record. |
| Plans | Existing GET /admin/plans and PUT /admin/plans. Fields: id, name, weight, max_depth_cap, max_contacts, ai_quota_per_day, zip_ttl_hours, resource_share. The current contract audits writes but has no audit-note or revision field; request these additions so the required note and conflict handling are recorded with the plan change. Plan values are entitlements, not billing. |
| Feature flags | Existing GET /admin/flags and PUT /admin/flags. Fields: name, enabled, audience (all, admins, user_ids), user_ids. Omitted flags remain unchanged; unknown flags are off. The service validates names/audiences and may take up to 30 seconds to refresh in other API processes. The current contract has no revision, audit note, or documented durable audit record; request atomic audited writes with expected_revision, audit_note, and a returned revision. The API returns user_ids for this audience: preserve them only as opaque in-memory form state when required for an update; never display, copy to a URL, or log them. |
| Live updates | Contract request: ADMIN-only resumable GET /admin/defaults/events with Last-Event-ID and events containing only revision and occurred_at. The client refetches the affected GET endpoint. Existing GET /events is USER-scoped and is not an ADMIN fallback. |
| Errors | Use the shared Error envelope and localize stable error codes, never raw server messages. Handle 400 validation, 401, 403, 409 revision conflict, 429, network, and 5xx. |

The static POC Save button only shows a toast; it is not evidence that settings or audit records were saved. Do not claim a successful mutation until the API returns success and its audit write is durable. The plans defaults are free (weight 1, depth cap 1, 100 contacts, AI quota 0/day, ZIP TTL 24h, share 0.2) and pro (weight 4, depth cap 3, 1,000 contacts, AI quota 100/day, ZIP TTL 72h, share 0.8), per the plan seed.

## Actions

- Refresh refetches defaults, plans, flags, and revision while retaining last-good values.
- Editing a default or editable cap stages a local diff. Validate required values and server-reported bounds before enabling Save; server validation remains authoritative. A cap reduction that would invalidate an existing user override is rejected without changing either the cap or the override.
- Save requires a non-empty audit note, submits only changed records with the expected revision, and waits for success. On 409 retain the draft, reload current data, and let ADMIN reconcile the diff.
- Discard removes the staged diff after confirmation. It does not roll back a completed server write.
- Plan and flag edits go only to their existing endpoints. Never assign a plan to a user from this view.
- Enabling group pools, changing quota/rate limits, plan caps, or a broadly targeted flag requires a confirmation that names the affected setting and warns that the change is global.

## Interactions

Load defaults, plans, and flags independently. Keep each section's last-good snapshot while it refreshes; do not replace missing or failed values with zero, false, or a POC value. Normalize durations to human-readable units while retaining the exact server value in the edit control. Percentages use 0–100 in the UI and the API fraction for person_lane_share. Show plan resource_share as a percentage. After a successful mutation, replace the draft with the returned snapshot and revision, show the audit confirmation, and refresh all relevant sections.

## States

### Ready

Show all loaded sections with their own updated time and the global revision. Mark unsaved fields as a draft and show the audit-note field. Display “Effective default” with a helper that per-user overrides can change the final value.

### Loading

For an initial load longer than 200 ms, show labelled skeleton rows and aria-busy=true. During refresh, preserve each last-good section and mark only that section busy. Keep Save disabled until required data and revision are loaded.

### Empty

An empty plans or flags response shows a section-specific explanation and a CTA to add a plan or flag draft. The CTA opens a draft editor and does not write. Defaults returning no required settings is an error, not an empty success. The mockup uses the same empty state for the view-level preview.

### Error

On initial failure show “Could not load defaults and plans” with Retry; identify which section failed. A failed refresh keeps last-good values and marks them stale. A failed mutation preserves the draft and audit note and states that no change was confirmed.

### Partial and stale

If only one endpoint fails, render the other valid sections and mark the failed section unavailable. After three failed event reconnects, poll the relevant GET endpoints every 30 seconds with backoff capped at two minutes. Mark a section stale 60 seconds after its last successful response. Never show stale settings as current or apply a draft to an unknown revision.

### Forbidden

On 401 close the event stream and follow the shell session flow. On 403 clear defaults, plans, flags, drafts, and cached revisions before showing the shared access-denied state.

## Live updates

When the proposed ADMIN event stream exists, subscribe to /admin/defaults/events with Last-Event-ID. Accept only event ID, revision, and occurred_at; refetch the affected section instead of trusting event values. If the stream is not available, use Refresh only until the event contract ships; then use the polling fallback above. Pause polling while the document is hidden and refresh on return. Close the stream and clear timers on sign-out or 403. Writes remain disabled while the current revision is unknown.

## Responsive

At 1440px use two columns for defaults and a full-width plans table and flags table. At 720px and below use one column for defaults and convert each plan/flag row into a labelled card. At 390px retain 16px gutters, stack the save actions, and allow English and Vietnamese labels to wrap without horizontal scrolling.

## Accessibility

Use one h1, semantic headings, labelled native inputs, table captions and scoped headers on desktop, and labelled plan cards on mobile. Pair every source/effective value with a text label and unit. Associate min/max and validation errors with their inputs; use aria-invalid on invalid fields. Announce loading, stale, conflict, save success, and failure through a polite live region; use role=alert for blocking errors. Keep keyboard focus visible, contrast at least 4.5:1 for text, and preserve reduced-motion behavior.

## i18n

Provide EN and VI text for every visible label, helper, empty-state, error, confirmation, and action. Interpolate {value}, {unit}, {time}, and {count}. Never render raw API messages.

| Key | EN / VI |
| --- | --- |
| admin.defaults.title | Defaults and plans / Mặc định và gói |
| admin.defaults.description | Review global defaults, plans, and feature flags. / Xem lại cấu hình mặc định toàn cục, gói và cờ tính năng. |
| admin.defaults.updated | Updated {time} / Cập nhật lúc {time} |
| admin.defaults.action.refresh | Refresh / Làm mới |
| admin.defaults.action.save | Save changes / Lưu thay đổi |
| admin.defaults.action.discard | Discard / Hủy thay đổi |
| admin.defaults.action.retry | Retry / Thử lại |
| admin.defaults.section.crawl | Crawl defaults / Mặc định thu thập |
| admin.defaults.section.quota | Quota and rate limits / Hạn mức và tốc độ |
| admin.defaults.section.downloads | Downloads / Tải xuống |
| admin.defaults.section.retries | Retry policy / Chính sách thử lại |
| admin.defaults.section.plans | Plans / Gói |
| admin.defaults.section.flags | Feature flags / Cờ tính năng |
| admin.defaults.field.name | Setting / Thiết lập |
| admin.defaults.field.source | Source value / Giá trị nguồn |
| admin.defaults.field.effective | Effective default / Mặc định hiệu lực |
| admin.defaults.field.bounds | Allowed range / Giới hạn cho phép |
| admin.defaults.field.userOverride | User override / Ghi đè của người dùng |
| admin.defaults.field.depth | Default depth / Độ sâu mặc định |
| admin.defaults.field.depthCap | Maximum depth cap / Giới hạn độ sâu tối đa |
| admin.defaults.field.hourly | Calls per hour / Lượt gọi mỗi giờ |
| admin.defaults.field.reserve | Interactive reserve / Dự phòng thao tác người dùng |
| admin.defaults.field.rate | Calls per second / Lượt gọi mỗi giây |
| admin.defaults.field.burst | Burst bucket / Hạn mức tăng đột biến |
| admin.defaults.field.laneSplit | Person and photo lane split / Tỷ lệ luồng người và ảnh |
| admin.defaults.field.zipTtl | ZIP link lifetime / Thời hạn liên kết ZIP |
| admin.defaults.field.retryMax | Maximum retries / Số lần thử lại tối đa |
| admin.defaults.field.retryBackoff | Initial retry backoff / Thời gian chờ thử lại ban đầu |
| admin.defaults.field.cooldown | Node cool-down / Thời gian nghỉ của nút |
| admin.defaults.field.changeCheck | Change-check interval / Chu kỳ kiểm tra thay đổi |
| admin.defaults.field.groupPools | Crawl group pools / Nhóm thu thập |
| admin.defaults.value.off | Off / Tắt |
| admin.defaults.value.on | On / Bật |
| admin.defaults.value.allowed | Allowed / Cho phép |
| admin.defaults.value.notAllowed | Not allowed / Không cho phép |
| admin.defaults.plan.name | Plan / Gói |
| admin.defaults.plan.weight | Weight / Trọng số |
| admin.defaults.plan.depthCap | Depth cap / Giới hạn độ sâu |
| admin.defaults.plan.contacts | Contact cap / Giới hạn liên hệ |
| admin.defaults.plan.ai | AI allowance per day / Hạn mức AI mỗi ngày |
| admin.defaults.plan.zip | ZIP retention / Thời hạn lưu ZIP |
| admin.defaults.plan.share | Resource share / Tỷ lệ tài nguyên |
| admin.defaults.flag.enabled | Enabled / Đã bật |
| admin.defaults.flag.audience | Audience / Đối tượng |
| admin.defaults.flag.specificUsers | Specific users / Người dùng được chọn |
| admin.defaults.auditNote | Audit note / Ghi chú kiểm toán |
| admin.defaults.confirm.title | Confirm global changes / Xác nhận thay đổi toàn cục |
| admin.defaults.confirm.body | These changes may affect all users. Review the staged values before saving. / Những thay đổi này có thể ảnh hưởng mọi người dùng. Hãy kiểm tra giá trị chờ lưu trước khi xác nhận. |
| admin.defaults.empty.title | No plans or flags configured / Chưa cấu hình gói hoặc cờ tính năng |
| admin.defaults.empty.body | Add a draft to begin. Nothing is saved until you confirm with an audit note. / Thêm bản nháp để bắt đầu. Chưa có gì được lưu cho đến khi xác nhận kèm ghi chú kiểm toán. |
| admin.defaults.error.title | Could not load defaults and plans / Không thể tải cấu hình mặc định và gói |
| admin.defaults.error.body | No changes were confirmed. Retry the failed section. / Chưa có thay đổi nào được xác nhận. Hãy thử tải lại phần bị lỗi. |
| admin.defaults.error.forbidden | You do not have access to this page / Bạn không có quyền truy cập trang này |
| admin.defaults.error.conflict | Settings changed elsewhere. Reload and reconcile your draft. / Cấu hình đã được thay đổi ở nơi khác. Hãy tải lại và đối chiếu bản nháp. |
| admin.defaults.state.loading | Loading configuration / Đang tải cấu hình |
| admin.defaults.state.stale | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| admin.defaults.state.saved | Saved and recorded in the audit trail / Đã lưu và ghi vào nhật ký kiểm toán |
| admin.defaults.state.validation | Check the highlighted values / Kiểm tra các giá trị được đánh dấu |
| admin.defaults.state.noAudit | Enter an audit note to save / Nhập ghi chú kiểm toán để lưu |

| admin.defaults.revision | Current revision {value} / Phiên bản hiện tại {value} |
| admin.defaults.preview.example | Illustrative sample values, not live configuration / Giá trị minh họa, không phải cấu hình đang chạy |
| admin.defaults.state.ready | Ready / Sẵn sàng |
| admin.defaults.state.empty | Empty / Trống |
| admin.defaults.state.error | Error / Lỗi |
| admin.defaults.state.unavailable | Unavailable / Không có dữ liệu |
| admin.defaults.state.draft | Draft / Bản nháp |
| admin.defaults.section.global | Global defaults / Mặc định toàn cục |
| admin.defaults.section.planCaption | Plan entitlement limits / Giới hạn quyền lợi theo gói |
| admin.defaults.section.flagCaption | Rollout controls / Điều khiển phát hành tính năng |
| admin.defaults.section.flagExamples | Example rows only / Chỉ là các hàng minh họa |
| admin.defaults.source.application | Application default / Mặc định ứng dụng |
| admin.defaults.source.quota | Quota policy / Chính sách hạn mức |
| admin.defaults.source.effective | Effective before user override / Có hiệu lực trước khi người dùng ghi đè |
| admin.defaults.field.hourlyCeiling | Hourly ceiling / Giới hạn mỗi giờ |
| admin.defaults.field.initialBackoff | Initial backoff / Thời gian chờ ban đầu |
| admin.defaults.field.photoLane | Photo lane / Luồng ảnh |
| admin.defaults.field.sourceDefault | Source default / Giá trị mặc định nguồn |
| admin.defaults.field.effectiveDefault | Effective default / Giá trị mặc định hiệu lực |
| admin.defaults.field.maxCap | Maximum cap / Giới hạn tối đa |
| admin.defaults.unit.calls | calls / lượt gọi |
| admin.defaults.unit.callsPerHour | calls per hour / lượt gọi mỗi giờ |
| admin.defaults.unit.callsPerSecond | calls per second / lượt gọi mỗi giây |
| admin.defaults.unit.tokens | tokens / token |
| admin.defaults.unit.hours | hours / giờ |
| admin.defaults.unit.minutes | minutes / phút |
| admin.defaults.unit.seconds | seconds / giây |
| admin.defaults.unit.attempts | attempts / lần thử |
| admin.defaults.unit.percent | percent / phần trăm |
| admin.defaults.save.changedCount | {count} changed fields / {count} trường đã thay đổi |
| admin.defaults.save.auditRequired | Enter an audit note to enable saving / Nhập ghi chú kiểm toán để bật nút lưu |
| admin.defaults.save.discardConfirm | Discard this draft? / Hủy bản nháp này? |
| admin.defaults.empty.addPlan | Add a plan draft / Thêm bản nháp gói |
| admin.defaults.empty.addFlag | Add a flag draft / Thêm bản nháp cờ tính năng |
| admin.defaults.notice.noUserIds | Specific-user targets are never displayed here. / Không hiển thị danh sách người dùng cụ thể tại đây. |
| admin.defaults.audit.help | Changes require server validation, a revision match, and a durable audit record. / Thay đổi cần được máy chủ xác thực, khớp phiên bản và ghi nhật ký kiểm toán bền vững. |

| admin.defaults.preview.static | Static controls do not save changes / Các điều khiển tĩnh không lưu thay đổi |
| admin.defaults.source.registry | Settings registry / Danh mục thiết lập |
| admin.defaults.unit.hoursShort | h / giờ |
| admin.defaults.unit.percentShort | % / phần trăm |
| admin.defaults.flag.audienceAll | All / Tất cả |
| admin.defaults.flag.audienceAdmins | Admins / Quản trị viên |

| admin.defaults.theme.dark | Dark control room / Bảng điều khiển tối |
| admin.defaults.theme.light | Light control room / Bảng điều khiển sáng |
| admin.defaults.state.loadingDetails | Reading sections / Đang đọc các mục |
| admin.defaults.helper.photoLaneRemaining | The photo lane receives the remaining share. / Luồng ảnh nhận phần tỷ lệ còn lại. |
| admin.defaults.helper.groupPoolsOff | Expensive; disabled by default. / Tốn tài nguyên; tắt theo mặc định. |
| admin.defaults.save.title | Staged save · audit required / Lưu bản nháp · cần ghi chú kiểm toán |

## Test IDs

| Element | data-testid |
| --- | --- |
| Page | admin-defaults-page |
| Refresh | admin-defaults-refresh |
| Defaults section | admin-defaults-settings |
| Setting row | admin-defaults-setting-{key} |
| Plans table | admin-defaults-plans |
| Plan row | admin-defaults-plan-{name} |
| Flags table | admin-defaults-flags |
| Flag row | admin-defaults-flag-{name} |
| Audit note | admin-defaults-audit-note |
| Save changes | admin-defaults-save |
| Discard draft | admin-defaults-discard |
| Confirmation dialog | admin-defaults-confirm |
| Loading state | admin-defaults-loading |
| Empty state | admin-defaults-empty |
| Error state | admin-defaults-error |
| Stale section | admin-defaults-stale |
| Forbidden state | admin-defaults-forbidden |
| Revision conflict | admin-defaults-conflict |

## Test hooks

Keep these IDs stable across themes and states. Verify that defaults never come from mock values, per-user data is not requested, user IDs are not rendered for flags, stale revisions do not save, a cap change cannot invalidate an existing override, and a failed audit leaves the draft unsaved.

## Out of scope

Application code, OpenAPI edits, user plan assignment, billing, per-user setting inspection, API keys or credentials, feature-flag evaluation, and any write that cannot be recorded durably in the audit trail.
