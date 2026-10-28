# Catalog reconciliation

The crawl cycle schedules a full reconciliation for each interested person
using the user's `reconcile_interval`, which defaults to 30 days. The setting
is user-overridable. A durable `reconcile` work item reads all pages from
`flickr.people.getPhotos`, then all pages from `flickr.favorites.getList`.
Both lists share the work item's start-time checkpoint.

The engine applies a diff only after the final favorites page succeeds. A
malformed response, failed page, or incomplete work item leaves the previous
catalog state in place. Photos remain stored with `tombstoned_at`; when a later
scan sees one again, its upsert clears the tombstone. Default photo selection
and download requests exclude tombstoned photos. Favorites keep their rows with
`removed_at`, and the default favorites list filters removed relationships.

Visibility limits what a Flickr key can see. A scan can tombstone missing public
photos. It can tombstone private, friends-only, or family-only photos only when
the scanning user's connected account is the photo owner. This prevents a
public or friend-level view from treating an inaccessible private item as
deleted. Missing favorite relationships are marked removed only by the person's
own connected account, because favorites visibility can also be limited.
Reconciliation never hard-deletes catalog rows.

When an observed photo changes visibility, the `photos` update emits one
transactional `photo.visibility_changed` outbox event. Its key is the photo ID;
its payload contains `photo_id`, `owner_nsid`, `from`, and `to`. The event is
created only when the stored visibility actually changes.
