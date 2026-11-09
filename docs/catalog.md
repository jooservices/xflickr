# Catalog people and photos

The catalog stores Flickr people and photo metadata in PostgreSQL. Crawl pages
ensure people exist before writing photos and other records with person foreign
keys. This insert uses `ON CONFLICT DO NOTHING`, ordered by NSID, so an
unchanged existing person is not row-locked. The page refreshes person metadata
after its dependent rows and observations are written; it locks only changed
rows in NSID order. Empty values preserve stored data, and an NSID placeholder
username cannot replace a real username. The public `UpsertPeople` operation
keeps both steps together for non-crawl callers.

People writes chunk at 500 rows inside the caller's transaction. Group and
person-group writers that may receive an unpaged Flickr list larger than 500
rows also chunk through a shared helper so every chunk shares the caller's
transaction; other writers still reject oversized batches. Photo IDs route
across 16 hash partitions, with indexes for owner, last update, and size
retrieval status.

Other PostgreSQL upsert batches accept up to 500 records and skip unchanged
records.
Photo equality uses Flickr's `last_update` value and a SHA-256 hash of the
source JSON. The adapter stores the zstd compressed source in a `photos.raw`
JSONB envelope with an explicit encoding and base64 data.

Contact relationships live in `contacts (person_nsid, contact_nsid)` with
idempotent upserts (`seen_at` refresh). The crawl `contacts` facet writes those
rows for each connected account's `flickr.contacts.getList` page. Before the
relationship write, `EnsurePeople` inserts any missing account subject with
`username = nsid` and `ON CONFLICT DO NOTHING`, so a connected account that
never had a `people` row still commits without overwriting a real identity.
Catalog `ListContacts` reads distinct contacts of the caller's connected
accounts for the contacts screen (REST routes arrive with the catalog REST
slice).

Size updates carry `none`, `provisional`, or `confirmed` status. A provisional
response cannot replace confirmed sizes. Tombstone updates preserve the first
recorded timestamp when repeated.

## Catalog read API

The authenticated read API exposes `GET /people`, `/people/{nsid}`,
`/people/{nsid}/photos`, `/favorites`, `/albums`, `/albums/{id}/photos`, and
`/photos/{id}`. The OpenAPI contract in `contracts/openapi/paths/catalog.yaml`
is the source for the generated server and web clients.

Every collection uses an opaque keyset cursor and a page size from 1 to 100
(default 50). People are listed when the user observed them or has at least one
visible photo from them. Favorites and `/albums` are limited to the user's
connected Flickr accounts. `/me/albums` lists metadata for known contacts only
when at least one non-tombstoned photo in the album is visible under the same
sharing policy. It returns complete `contact_owners` options and accepts an
`owner_nsid` filter. One request checks at most 128 album photo memberships per
album; its cursor resumes an album probe when that bound is reached. It does
not return a photo count. Photo rows in every response
use the shared sharing policy and exclude tombstones; hidden records are
omitted from lists and return the same 404 as missing records for detail reads.
Counts on photo-backed list endpoints include only rows visible to the signed-in
user. An album detail may report only the number of photos in its first bounded
page as “N photos loaded”, never an album total.

Photo responses contain tags as `labels`, stored Flickr size entries and their
`sizes_status`, plus `ai_status` (`ready` when a caption enrichment exists,
otherwise `none`). Caption text, Flickr API credentials, the source payload,
photo secrets, and content hashes are not returned.

See [Deleted entities and restricted facets](entity-state.md) for global gone
markers, caller-specific restrictions and request suppression.

## Crawl provenance and recovery

People and photo upserts from the crawl now record the work user's actual
connected account in the same page transaction. See [crawl observations](crawl-observations.md)
for the centralized record-kind inventory and the guarded, batched backfill command.
Backfilled `first_seen_at` values are approximate: stored `last_seen_at` when
present, otherwise backfill run time. Only new crawl observations record the exact
first-observed time; reruns never overwrite an existing timestamp.
