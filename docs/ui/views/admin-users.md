# Admin users

## Purpose

Let ADMIN manage account access and plans while providing a read-only support summary. The view must never impersonate a user or expose credentials, Flickr content or private account data.

## Route and access

- Route `/admin/users` in the ADMIN shell; dark control-room theme by default and the existing light theme as an option.
- Require an authenticated ADMIN session before loading. A 401 follows shell sign-in; a 403 clears cached user data and shows the shared forbidden state.
- The shell owns navigation, theme, language and global live controls. This view owns search, pagination, invite, account actions, the support drawer and freshness.

## Layout

- Header: “Users”, total count, Live/Stale timestamp, Refresh and Invite user.
- User table columns: display name/email, role, plan, API key count and safe state summary, Flickr-account count, last seen, account status and row actions.
- Search reuses the server's case-insensitive email query without placing the query in analytics or telemetry. Show the email because it is the account locator; keep it out of the support drawer URL and logs.
- Selecting a row opens a read-only support drawer with locale, creation/last-seen times, plan summary, key counts/status and Flickr-account count. Never show key IDs, masked key values, secrets, tokens, NSIDs, photo records, crawl results, session tokens or private content.
- Invite opens a focused dialog for email and user plan. Invitations create USER accounts only; admin elevation is a separate, audited row action.
- Row actions are Change role, Change plan, Disable/Enable and Open support details. Disable and privilege/plan changes require a confirmation with the effect and target identity. Never show an impersonation or “log in as user” action.

## Data

The view is ADMIN-only. Existing API fields are distinguished from required additions below. Last seen is the latest successful authenticated activity timestamp in UTC; “Never” is distinct from an unavailable value. Key and Flickr-account fields are aggregate counts and safe states, not identifiers or credential material.

## Data and API

Paths are relative to `/api/v1`. A **contract request** is a JOOXF-186 implementation dependency; this design ticket does not change OpenAPI or application code.

| Content | Source, fields and action |
| --- | --- |
| User list | Existing ADMIN-only `GET /admin/users?q=&page=&page_size=` returns `items,total,page,page_size`; `id,email,role,locale,disabled,created_at`. Extend its response contract with `support_ref, display_name, plan{id,name}, key_summary{total,valid,attention}, flickr_account_count, last_seen_at, invitation_state` (`none, pending, accepted, expired, delivery_failed`). Derive Active/Disabled/Invited labels from `disabled` and invitation state; `support_ref` is opaque and is not the numeric mutation ID. These required table fields are absent today. Paginate with the existing page parameters; search email using the existing case-insensitive `q`. |
| Plans | Existing ADMIN-only `GET /admin/plans` returns `id,name,weight,max_depth_cap,max_contacts,ai_quota_per_day,zip_ttl_hours,resource_share`. Use it to label plan assignment choices and show the selected plan's effective limits. Do not edit plan definitions from this view. |
| Change role/disabled state | Existing ADMIN-only `PATCH /admin/users/{id}` accepts `role` and/or `disabled`. It rejects changes that demote/disable the last active admin with 409 `last_admin`; a 401/403/404 is handled without optimistic state. Do not allow self-disable in the UI. |
| Change user plan | Existing ADMIN-only `PUT /admin/users/{id}/plan` accepts `{plan_id}`; 404 means user or plan is missing. Confirm the old/new plan and effective limits, then refresh the affected row. |
| Invite | No invite endpoint exists. **Contract request:** ADMIN-only `POST /admin/users/invitations` with `{email, plan_id, locale}`; invite USER role only. Return opaque `invitation_ref, state, expires_at`; never return or display the one-use invitation token. Prevent duplicates and audit successful creation. |
| Audit prerequisite | Existing role/disable and plan routes are described as audited, but `docs/admin-users.md` says the identity role currently wires `audit.Discard` until durable audit storage is connected. JOOXF-186 must use the canonical audit trail and fail closed if the durable record cannot be written before enabling mutations. Do not add a second audit table or pretend an acknowledged action was durably recorded. |
| Existing events | Authenticated `GET /events` exists and is USER-scoped; it is not a source for administrator-wide user or last-seen updates. |
| ADMIN live updates | **Contract request:** ADMIN-only `GET /admin/users/events`, resumable with `Last-Event-ID`; aggregate `admin.users.changed` events carry `user_ref, changed_fields, observed_at`, and activity events carry only `user_ref,last_seen_at`. No email, session, key, Flickr or photo data in events. |
| Errors | Map stable codes `last_admin, duplicate_invitation, invitation_delivery_failed, audit_unavailable` to localized copy. Never render raw server messages. Existing shared error envelope remains authoritative. |

The existing USER session and API-key endpoints must not be used for support lookup. The support drawer is built from the ADMIN summary projection; it has no session creation or user-context action.

## Actions

- Refresh reloads the current page and support summary while preserving search/page.
- Invite validates email and plan, then requests a confirmation before sending the invitation. The dialog labels the role as USER. Invitation delivery failure leaves no active account and supports a safe retry after refresh.
- Change role, change plan and disable/enable each require a separate confirmation and a durable audit capability. Role elevation requires an extra privilege warning; disable warns that sign-in stops and all current sessions are rejected. Never permit self-disable. The server enforces the last-active-admin rule.
- Open support details is read-only. It exposes no impersonation, session revocation, credential reset, secret reveal or data browsing action.

## Interactions

The support drawer uses `?user=<opaque-ref>`; closing it removes that query value. Search is debounced, resets to page 1 and sends only the user's explicit email query to `GET /admin/users`. A user reference that is missing or unauthorized shows the same not-found state and reveals no existence information. Mutations update the row only after a successful response and durable audit acknowledgement.

## States

### Ready

Show the paginated table and selected plan/status labels. When the drawer opens, show only the safe support summary. Status always has text; do not rely on color alone.

### Loading

Show skeleton table rows and drawer placeholders with `aria-busy="true"`. Keep last-good data visible during a page refresh.

### Empty

If there are no accounts, show “No users yet” and the Invite user action. If search returns no matches, explain that and offer Clear search.

### Error

An initial list failure shows localized copy and Retry. A plan-choice failure keeps list browsing available but disables Change plan. A failed mutation leaves the server-confirmed row unchanged and requires refresh before retrying.

### Partial and stale

If the list succeeds but plans, support details or live updates fail, keep successful panels usable and mark only affected data stale. Show the last successful timestamp and Refresh; do not present old role/disabled state as current.

### Forbidden

For 403, clear list, drawer, plan and invitation data, close any dialog and show the shared forbidden state without revealing user existence.

## Live updates

Load the current page and plan list before opening the ADMIN stream. Show Live only after SSE connects and resume with `Last-Event-ID`. Apply user changes by opaque reference; refetch the affected row or current page if fields are missing/out of order. If SSE fails, poll the visible page and open support summary every 30 seconds, back off to 60 seconds on errors, pause in hidden tabs and refresh on return. Mark data stale after 60 seconds without an observation. Never use USER `GET /events` for administrator-wide updates.

## Responsive

At 1440px show the table with a right-side support drawer and a centered invite/confirmation dialog. At 390px convert rows to labeled cards, stack filters, and use a full-screen drawer/dialog. Keep destructive action explanations and email readable without page-level horizontal scrolling.

## Accessibility

Use one h1, semantic table headers, labeled search and plan controls, keyboard-operable actions, visible focus and a focus-trapping dialog. Return focus to the triggering row/button when the drawer or dialog closes. Announce save/invite outcomes politely; set `aria-busy` during requests. Pair status with text, preserve contrast and honor reduced motion.

## i18n

Every label, role, plan/status action, confirmation, empty state and error has EN / VI copy. Interpolate `{count}`, `{email}`, `{current_plan}`, `{new_plan}` and `{time}`. Localize stable error codes; never render raw API messages.

| Key | EN / VI |
| --- | --- |
| admin.users.title | Users / Người dùng |
| admin.users.description | Account access, plans and safe support details. / Quyền truy cập, gói dịch vụ và thông tin hỗ trợ an toàn. |
| admin.users.action.refresh | Refresh / Làm mới |
| admin.users.action.invite | Invite user / Mời người dùng |
| admin.users.live | Live / Đang cập nhật |
| admin.users.stale | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| admin.users.search | Search email / Tìm email |
| admin.users.search.clear | Clear search / Xóa tìm kiếm |
| admin.users.table.user | User / Người dùng |
| admin.users.table.role | Role / Vai trò |
| admin.users.table.plan | Plan / Gói |
| admin.users.table.keys | API keys / Khóa API |
| admin.users.table.accounts | Flickr accounts / Tài khoản Flickr |
| admin.users.table.lastSeen | Last seen / Hoạt động gần nhất |
| admin.users.table.status | Status / Trạng thái |
| admin.users.table.actions | Actions / Thao tác |
| admin.users.role.user | User / Người dùng |
| admin.users.role.admin | Admin / Quản trị viên |
| admin.users.status.active | Active / Đang hoạt động |
| admin.users.status.disabled | Disabled / Đã vô hiệu hóa |
| admin.users.status.invited | Invitation pending / Đang chờ lời mời |
| admin.users.keys.summary | {count} keys · {valid} valid / {count} khóa · {valid} hợp lệ |
| admin.users.accounts.count | {count} accounts / {count} tài khoản |
| admin.users.lastSeen.never | Never / Chưa từng |
| admin.users.action.details | Support details / Chi tiết hỗ trợ |
| admin.users.action.changeRole | Change role / Đổi vai trò |
| admin.users.action.changePlan | Change plan / Đổi gói |
| admin.users.action.disable | Disable account / Vô hiệu hóa tài khoản |
| admin.users.action.enable | Enable account / Kích hoạt tài khoản |
| admin.users.action.confirmRole | Change {email} to {role}? / Đổi vai trò của {email} thành {role}? |
| admin.users.action.confirmPlan | Change {email} from {current_plan} to {new_plan}? / Đổi gói của {email} từ {current_plan} sang {new_plan}? |
| admin.users.action.confirmDisable | Disable {email} and reject all current sessions? / Vô hiệu hóa {email} và từ chối mọi phiên hiện tại? |
| admin.users.action.confirmEnable | Enable sign-in for {email}? / Cho phép {email} đăng nhập? |
| admin.users.action.selfDisable | You cannot disable your own account here. / Bạn không thể tự vô hiệu hóa tài khoản tại đây. |
| admin.users.action.lastAdmin | The last active administrator cannot be demoted or disabled. / Không thể hạ quyền hoặc vô hiệu hóa quản trị viên đang hoạt động cuối cùng. |
| admin.users.action.audit | This action is recorded in the audit trail. / Thao tác này được ghi vào nhật ký kiểm tra. |
| admin.users.invite.title | Invite user / Mời người dùng |
| admin.users.invite.email | Email address / Địa chỉ email |
| admin.users.invite.plan | Starting plan / Gói ban đầu |
| admin.users.invite.role | Role · User / Vai trò · Người dùng |
| admin.users.invite.send | Send invitation / Gửi lời mời |
| admin.users.invite.confirm | Send an invitation to {email}? / Gửi lời mời đến {email}? |
| admin.users.invite.success | Invitation sent / Đã gửi lời mời |
| admin.users.invite.pending | Invitation pending / Lời mời đang chờ |
| admin.users.drawer.title | Support details / Chi tiết hỗ trợ |
| admin.users.drawer.created | Account created / Tài khoản được tạo |
| admin.users.drawer.locale | Language / Ngôn ngữ |
| admin.users.drawer.noImpersonation | Support is read-only. User sessions and private content are not available. / Chỉ hỗ trợ xem. Không truy cập phiên hoặc nội dung riêng tư của người dùng. |
| admin.users.empty.title | No users yet / Chưa có người dùng |
| admin.users.empty.body | Invite a user to get started. / Mời người dùng để bắt đầu. |
| admin.users.empty.filtered | No users match this email / Không có người dùng khớp email này |
| admin.users.loading | Loading users / Đang tải danh sách người dùng |
| admin.users.error.load | Could not load users / Không tải được danh sách người dùng |
| admin.users.error.plans | Plan choices are unavailable / Không có lựa chọn gói |
| admin.users.error.action | The action failed. Refresh before retrying. / Thao tác thất bại. Hãy làm mới trước khi thử lại. |
| admin.users.error.lastAdmin | The last active administrator cannot be demoted or disabled. / Không thể hạ quyền hoặc vô hiệu hóa quản trị viên đang hoạt động cuối cùng. |
| admin.users.error.invite | Invitation could not be sent / Không thể gửi lời mời |
| admin.users.error.audit | The audit trail is unavailable; no change was made. / Nhật ký kiểm tra không khả dụng; chưa có thay đổi nào được thực hiện. |
| admin.users.error.forbidden | Admin access required / Cần quyền quản trị viên |

## Test IDs

`admin-users-page, admin-users-live-status, admin-users-refresh, admin-users-invite, admin-users-search, admin-users-clear-search, admin-users-table, admin-users-total, admin-users-page-{number}, admin-users-row-{id}, admin-users-role-{id}, admin-users-plan-{id}, admin-users-key-summary-{id}, admin-users-account-count-{id}, admin-users-last-seen-{id}, admin-users-status-{id}, admin-users-row-actions-{id}, admin-users-details-{id}, admin-users-change-role-{id}, admin-users-change-plan-{id}, admin-users-disable-{id}, admin-users-enable-{id}, admin-users-support-drawer, admin-users-invite-dialog, admin-users-invite-email, admin-users-invite-plan, admin-users-invite-submit, admin-users-confirmation, admin-users-loading, admin-users-empty, admin-users-empty-filtered, admin-users-error, admin-users-stale, admin-users-forbidden`.

## Test hooks

The page root exposes `data-testid="admin-users-page"`; table, row actions, support drawer, invite dialog and all states use the stable hooks listed above.

## Out of scope

Application/OpenAPI implementation, editing plan definitions, session impersonation or creation, password reset, key/secret display or rotation, Flickr account management, private photo/content browsing, session revocation outside account disable, audit viewer UI, bulk actions, and changes to other views.
