# Photo viewer

JOOXF-171 adds a testable photo viewer component for later use by the gallery.
The viewer accepts the gallery's ordered photo IDs and selected index, requests
only the current photo through the generated `GET /photos/{id}` client, and
shows its title and a recorded photo size URL. Provisional listing URLs come
from `url`; confirmed size URLs come from `source`. Previous and next controls
wrap through the supplied IDs. The dialog traps focus, closes with Escape, and
returns focus to its opener or the gallery heading.

This slice is exercised through a test-only browser harness. It is not linked
from the product router because JOOXF-170 has not supplied the photo tile and
gallery list. It uses the current catalog response, which does not contain all
fields in the approved full viewer contract. Owner, caption, tags, size details,
favorites, tray actions, downloads, live updates, and touch swipe are deferred.
The current API returns 404 for both missing and inaccessible photos, so the
viewer uses one unavailable state for both.

Component/MSW tests cover loading, empty, success, preview failure, retryable
errors, 404, 401 session expiry, navigation, translations, and focus behavior.
The mocked Playwright scenario covers keyboard use, accessibility, and desktop
and mobile reflow. The sharing visibility filter used by photo detail reads is
supported by a concurrent observations index migration; its behavior and the
API contract do not change.
