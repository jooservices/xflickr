-- name: CountSharedRecords :one
-- Photo record ids are decimal Flickr ids; the regex keeps the bigint cast safe
-- and lets the join probe the photos primary key instead of hashing every photo.
SELECT count(DISTINCT o.record_id)::bigint AS shared_record_count
FROM observations o
JOIN sharing_settings ss ON ss.user_id = o.user_id AND ss.enabled
JOIN photos p ON p.id = o.record_id::bigint
WHERE o.user_id = $1
  AND o.record_kind = 'photo'
  AND o.record_id ~ '^[0-9]{1,18}$'
  AND p.visibility = 'public';
