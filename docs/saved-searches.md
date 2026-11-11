# Saved searches

The first saved-search slice stores and reads one query for one XFlickr user.
It is independent of the search engine and does not expose an HTTP endpoint.
Saved searches are private to their owner; multiple rows may contain the same
query.

## Stored data

`saved_searches` stores an identity, the owning `user_id`, the query text, and
the PostgreSQL creation time. Deleting a user cascades to that user's saved
searches. The table contains no filters, display name, photo data, or search
results.

The application trims leading and trailing Unicode whitespace. A query must
contain 1–1,000 Unicode code points after trimming and must not contain NUL.
The database also enforces the stored query length. Invalid user IDs, saved
search IDs, and query values are rejected before persistence.

## Internal operations

`app.SavedSearches.Create` validates and stores a query through
`app.SavedRepository`. PostgreSQL assigns the ID and creation time and returns
them with the trimmed query. `app.SavedSearches.Get` reads by the pair
`(user_id, id)`. A missing ID and a row owned by another user both return
`app.ErrNotFound`.

The `(user_id, id)` index supports owner-scoped reads and user deletion. This
slice does not add list, update, delete, search, HTTP, or UI behavior. The
search view remains pending in the UI catalogue.
