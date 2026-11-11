# Reading queue metric samples

`GET /admin/metrics` lets administrators read stored samples for
`queue.jobs_available`, the count of ready River jobs for a queue. It is not
the lane backlog from `work_items`.

The request requires `series`, `queue`, `role`, `node`, `from`, `to`, and
`step`. This slice supports only `series=queue.jobs_available` and `step=1m`.
Queue, role, and node labels are matched together; samples are never combined
across queues, roles, or nodes. Dimension labels must be valid UTF-8, non-empty,
and no more than 128 bytes. Whitespace is preserved.

The RFC3339 `from` and `to` values must be at exact minute boundaries. The
query covers `[from, to)`, spans no more than 60 minutes, and cannot end after
the current UTC minute. A successful response returns the stored timestamps and
values in ascending order. No rows produce `points: []`; missing samples are
never filled with zero. Invalid input returns the same `400` error, and storage
failures return a generic `500` response.

The reader hashes the exact `{queue, role, node}` labels with the collector's
canonical label hash and reads only `metric_samples`. It orders and bounds the
query through the existing unique index on `(name, labels_hash, bucket)` and
returns at most 60 rows. It does not read `river_job` or catalog tables. Use the
`scheduler` role when a cluster-wide collector is needed; collectors are not
summed.

This slice adds no migration and leaves `metrics.retention_days=30` unchanged.
Rollups, 14-day retention, other series and time steps, live quota or node
state, UI integration, SSE, and charts remain deferred. The existing River
collector's scan behavior is unchanged.
