# OneDrive folder adapter

This slice implements `EnsureAlbum` against the signed-in user's OneDrive App
Folder with Microsoft Graph v1.0. It is limited to personal Microsoft accounts
and the delegated `Files.ReadWrite.AppFolder` permission. It does not support
SharePoint or broader file permissions.

Each call resolves `/me/drive/special/approot` again, then looks up or creates
one direct child folder. The returned `RemoteAlbumID` is the Graph driveItem ID.
Names are trimmed and validated as one folder name; path separators,
OneDrive-invalid characters, names beginning with `~`, and names ending with a
period are rejected before any request. URL path segments are escaped
independently.

HTTP 401 and 429 map to `app.ErrAuthentication` and `app.ErrRateLimited`.
Other Graph failures and malformed folder responses return contextual errors
with status codes, without response bodies or credentials. Responses are limited
to 64 KiB, requests do not follow redirects, and request contexts reach HTTP.

This adapter is not registered as a `StorageProvider`. Upload, existence,
deletion, capabilities, OAuth, token persistence, and the shared provider
contract suite remain for later slices. Tests use only `httptest`.

References:

- [OneDrive permissions](https://learn.microsoft.com/en-gb/onedrive/developer/rest-api/concepts/permissions_reference?view=odsp-graph-online)
- [App Folder](https://learn.microsoft.com/graph/onedrive-sharepoint-appfolder)
- [Create a Graph folder](https://learn.microsoft.com/en-us/graph/api/driveitem-post-children?view=graph-rest-1.0)
