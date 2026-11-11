# Group membership crawl

The `groups` facet uses the existing `flickr.people.getGroups` request and
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

This slice validates group membership responses only. It does not add REST or
UI behavior, fetch group metadata with `groups.getInfo`, select another Flickr
method, or crawl group pools. The existing `group_pools_enabled` setting stays
disabled by default.
