# Contacts depth slice

This slice gives the Contacts route a real crawl-depth summary while the
user-scoped contacts list API is unavailable.

## Data source

The page reads `GET /me/crawl/settings` through the generated API client and
the existing crawl settings query. The server derives the user from the
session. The page displays only the response's `depth`; it does not supply a
default or infer contacts from another endpoint.

## States

- Loading announces that contacts are loading and does not show a depth.
- Success shows the effective depth and says the contacts list is not available
  yet. This is not an empty state.
- Request or response-shape errors show translated text and a Retry button.
- HTTP 401 follows the API client's shared expired-session flow.
- HTTP 403 hides any cached depth and shows translated access-denied text with
  a link to the home page.

The available states have English and Vietnamese copy. The page reuses the
Crawl page layout styles and does not call Flickr or issue a list request.

## Deferred work

The contacts-interest endpoint, contact cards, counts, search, filters,
sorting, starring, live updates, and the Contact page remain future slices.
This page adds no controls or links for those features and does not claim that
the user's contacts list is empty.
