# Photo viewer

JOOXF-407 connects the photo viewer to the USER Library. Activating a photo
tile opens an overlay without changing the route. The viewer follows the
currently visible gallery order, fetches detail only for the selected photo,
supports previous/next keyboard and touch navigation, and returns focus to the
invoking tile when closed.

The current `GET /photos/{id}` catalog response supplies the title, owner NSID,
description, tags, size status, dimensions, and recorded photo URL. The viewer
shows those available fields, links the owner and tags to their current routes,
and labels sizes as provisional or confirmed. The underlying catalog API has no
owner display name, AI caption text, EXIF, Flickr page URL, favorited-by data,
tombstones, or live photo events, so the viewer does not invent or display those
fields. JOOXF-177 owns the real tray and download flow; local Library selection
is not presented as adding a photo to that tray.

The approved [lightbox view](ui/views/user-lightbox.md) remains the full product
target. This slice does not change OpenAPI or claim its unavailable actions and
states as complete. The viewer uses the existing generated detail client and
the existing catalog visibility behavior, which returns the same 404 response
for missing and inaccessible photo IDs.

Component/MSW tests cover viewer state and keyboard/touch interaction. The
mocked Playwright scenario opens a photo from Library, navigates, checks
accessibility, and verifies focus and route restoration.
