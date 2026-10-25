# Download progress events

Authenticated clients connect to `GET /events` for user-scoped server-sent
events. The API instances subscribe to the shared Redis event channel and fan
events out through one in-memory hub per instance.

The stream carries:

- `download.progress` when a download is created or an item settles.
- `download.ready` after the ZIP parts have been recorded.
- `crawl.progress` after a crawl control change (star, select, pause, resume)
  succeeds. Publishing from the crawl engine itself is deferred to the ticket
  that constructs the engine in a role; its `ProgressNotifier` hook is ready.

Each event frame includes an `id`. A reconnect can send that value in
`Last-Event-ID`; the API replays newer events retained in that instance's
bounded in-memory history. A new connection without `Last-Event-ID` receives
live events only, with no replay.

Media work observes the configured BlobStore capacity and, for local storage,
the underlying filesystem's available space. When the BlobStore reports full
or filesystem use exceeds 90%, new download requests return
`temp_storage_full` and the scheduler stops pumping media work.
