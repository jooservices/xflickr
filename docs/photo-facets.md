# Optional photo facets

XFlickr can collect five Flickr API facets after `getSizes` has confirmed the
photo for its current `last_update`:

| Setting | Flickr method | Default |
| --- | --- | --- |
| `photo_info_enabled` | `flickr.photos.getInfo` | on |
| `photo_exif_enabled` | `flickr.photos.getExif` | on |
| `photo_contexts_enabled` | `flickr.photos.getAllContexts` | on |
| `photo_comments_enabled` | `flickr.photos.comments.getList` | off |
| `photo_favorites_enabled` | `flickr.photos.getFavorites` | off |

These application settings are switchable through the settings service and do
not allow per-user overrides. Optional facets run only for photos owned by a
starred or actively selected contact. The scheduler always orders size jobs
before optional facets. Size jobs use the `sync` queue; optional jobs use the
separate `sync-low` queue and the same photo quota class.

Each `photo.facet` job is deduplicated by photo and facet. The worker validates
the photo and key IDs, confirms the current size result, fetches the selected
facet, then stores data and completion status in one transaction. A newer photo
version remains eligible because completion is tied to the `last_update` read
before the API call. `getInfo` and `getExif` receive the catalog secret when
available; returned `secret` and `originalsecret` values are stripped before
storage.

Dedicated tables store the result shapes:

- `photo_info` and `photo_exif` keep the typed Flickr response as JSONB.
- `photo_contexts` stores set and pool memberships by ID.
- `photo_comments` stores comments and author/date metadata.
- `photo_favorites` stores favorite users and dates.
- `photo_facet_status` records the completed Flickr photo version per facet.

Comments and favorites request 50 records per page. Each page uses a separate
quota permit, and the worker stops after 200 pages to bound an unexpectedly
large response. Flickr not-found and permission errors are terminal for the
current version and store an empty result; other API or storage errors are
retryable.

The app-level settings default to `info`, `exif`, and `contexts` on, and
`comments` and `photo_favorites` off. Change them through the admin settings
service. See [Photo lane selection](photo-lane.md) for queue ordering and query
details.
