# Group membership crawl

After the favorites probe of `ChangeCheck`, group refresh uses
`flickr.people.getPublicGroups` with the contact NSID and `per_page=500`.
Initial group crawl still uses `flickr.people.getGroups`. Existing work items
continue with their saved method and cursor. Both methods use the existing
page flow. A non-null `groups` object is required. A `group` array may be empty
or omitted when the response is terminal. An empty page is an error when
`pages` reports that more pages remain. A null group list, malformed JSON, or a
value with the wrong JSON type is an error.

Every group must include an `nsid` and `name` that remain non-empty after
trimming whitespace. The crawler stores their original values. It validates the
whole response before returning catalog rows, so one invalid member rejects
the entire page.

The engine sends a decode error through its existing failure and retry flow. It
does not upsert groups or memberships, advance the cursor, or finish the page.
Valid pages continue through the existing transactional group and membership
upserts.

This slice changes only group refresh method selection. It does not add REST
or UI behavior. Group metadata via `groups.getInfo` remains deferred until
metadata preservation rules are defined; group pools and per-user/group
preferences also remain deferred. The existing `group_pools_enabled` setting stays
disabled by default.
