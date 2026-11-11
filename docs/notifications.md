# Notifications storage

The `notifications` table stores notification records owned by one XFlickr
user. Its `user_id` foreign key references `users.id`; deleting a user deletes
that user's notifications. The database assigns each notification's identity
and creation time. `read_at` is nullable, with `NULL` representing an unread
notification.

The table stores `kind`, `title`, `body`, and `level` as required text fields.
It does not constrain event kinds to an enum while event sources are still
being established. The `(user_id, created_at DESC, id DESC)` index supports
owner-scoped newest-first pagination with a stable tie order.

This storage slice does not create or read notifications through an API or a
user interface. It also defers unread counts, delivery channels, outbox
consumers, retention, and pagination queries. Those behaviors require their
own application and adapter work; the drawer's proposed API payload is not
part of this schema.
