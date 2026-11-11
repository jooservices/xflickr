# Notifications panel

The USER and ADMIN shells open the notifications drawer from their bell button.
The drawer currently shows the shared “Not available yet” message because the
notification API is not available.

Clicking the scrim outside the drawer closes it. Clicks on the drawer content do
not close it. The Close button and Escape also dismiss it; each close returns
focus to the bell. The drawer keeps its existing focus trap through `useOverlay`.

The notification list, unread count, mark-read actions, live updates, and
loading, empty, error, and forbidden states are deferred. This slice makes no
notification requests and does not treat the unavailable message as an empty
notification list.
