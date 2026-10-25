# Plans

Plans store user entitlements and configurable product limits. The initial
catalog seeds `free` with scheduler weight 1 and resource share 0.2, and `pro`
with weight 4 and share 0.8. Each new user starts on `free`.

Administrators can read and update plan definitions at `/admin/plans`, then
assign a plan to a user with `PUT /admin/users/{id}/plan`. The plan lookup
service caches each user's plan for 30 seconds and clears affected entries when
an administrator updates the catalog or assigns a new plan. Plan updates and
user assignments are written to the audit event table.

Plan weights, depth caps, contact limits, daily AI quotas, ZIP retention, and
resource shares are entitlements only. Billing and payment processing are not
part of this feature.
