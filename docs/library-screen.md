# Library screen

The Library route currently uses the generated catalog favorites endpoint and
its opaque cursor. It requests up to 100 favorites at a time, merges duplicate
photo IDs, and virtualizes justified rows with TanStack Virtual. The row layout
uses dimensions from an available catalog size; photos without a size URL use
a neutral placeholder.

The screen supports filtering the favorites already loaded by contact, sizes
readiness, and AI caption readiness. It labels the loaded range explicitly and
does not claim that its loaded count or contact choices describe the full
library. Selection stays in the page while filters and cursor pages change.
The tray is submitted through the existing `POST /downloads` operation.

The approved [Library view spec](ui/views/user-library.md) describes the full
target. Completing it needs a bounded all-library cursor operation and server
facets so filters, counts, kind/depth choices, sorting, and URL state cover the
whole visible library. Library event updates and polling also need the
corresponding event contract. Until those operations exist, the page does not
invent records, aggregate all contacts in the browser, or issue full-table
counts.

The mocked browser suite loads 5,000 favorites through 50 cursor requests,
scrolls to the final photo, and asserts that fewer than 50 photo cards are
mounted at once.
